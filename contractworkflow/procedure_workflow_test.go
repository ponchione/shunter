package contractworkflow

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestLoadContractFileRejectsInvalidProcedureProductSchemas(t *testing.T) {
	tests := []struct {
		name       string
		product    schema.ProductSchemaExport
		result     bool
		wantDetail string
	}{
		{
			name: "args invalid type",
			product: schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "message_id", Type: "notAType"},
			}},
			wantDetail: `procedures.archive_message.args.columns.message_id type "notAType" is invalid`,
		},
		{
			name: "args empty column name",
			product: schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "", Type: "uint64"},
			}},
			wantDetail: "procedures.archive_message.args.columns name must not be empty",
		},
		{
			name: "args duplicate column name",
			product: schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "message_id", Type: "uint64"},
				{Name: "message_id", Type: "bool"},
			}},
			wantDetail: `procedures.archive_message.args.columns name "message_id" is duplicated`,
		},
		{
			name: "result invalid type",
			product: schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "archived", Type: "notAType"},
			}},
			result:     true,
			wantDetail: `procedures.archive_message.result.columns.archived type "notAType" is invalid`,
		},
		{
			name: "result empty column name",
			product: schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "", Type: "bool"},
			}},
			result:     true,
			wantDetail: "procedures.archive_message.result.columns name must not be empty",
		},
		{
			name: "result duplicate column name",
			product: schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
				{Name: "archived", Type: "bool"},
				{Name: "archived", Type: "uint64"},
			}},
			result:     true,
			wantDetail: `procedures.archive_message.result.columns name "archived" is duplicated`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := workflowContractFixture()
			procedure := shunter.ProcedureDescription{Name: "archive_message"}
			if tt.result {
				procedure.Result = &tt.product
			} else {
				procedure.Args = &tt.product
			}
			contract.Procedures = []shunter.ProcedureDescription{procedure}
			path := writeContractFixture(t, t.TempDir(), "invalid-procedure.json", contract)

			_, err := LoadContractFile(path, "procedure contract")
			if err == nil {
				t.Fatal("LoadContractFile accepted invalid procedure product schema")
			}
			if !strings.Contains(err.Error(), "invalid module contract JSON: procedure contract") ||
				!strings.Contains(err.Error(), tt.wantDetail) {
				t.Fatalf("LoadContractFile error = %v, want validation context %q", err, tt.wantDetail)
			}
		})
	}
}

func TestPrepareProcedureCallRequestUsesDeclaredSchemaAndRejectsMismatches(t *testing.T) {
	args := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "message_id", Type: "uint64"},
		{Name: "archive", Type: "bool"},
	}}
	contract := workflowContractFixture()
	contract.Procedures = []shunter.ProcedureDescription{
		{Name: "archive_message", Args: &args},
		{Name: "schema_less"},
	}

	request, err := PrepareProcedureCallRequest(contract, " archive_message ", []byte(`{"archive":true,"message_id":42}`))
	if err != nil {
		t.Fatalf("PrepareProcedureCallRequest: %v", err)
	}
	if request.Name != "archive_message" {
		t.Fatalf("request name = %q, want canonical procedure name", request.Name)
	}
	decoded, err := bsatn.DecodeProductValueFromBytes(request.Arguments, productTableSchema(t, "archive_message_args", args))
	if err != nil {
		t.Fatalf("decode prepared procedure arguments: %v", err)
	}
	if len(decoded) != 2 || decoded[0].AsUint64() != 42 || !decoded[1].AsBool() {
		t.Fatalf("decoded procedure arguments = %+v, want [42 true] in schema order", decoded)
	}
	direct, err := EncodeProcedureArguments(contract, "archive_message", []byte(`{"message_id":42,"archive":true}`))
	if err != nil {
		t.Fatalf("EncodeProcedureArguments: %v", err)
	}
	if !bytes.Equal(direct, request.Arguments) {
		t.Fatalf("direct procedure encoding = %x, prepared request = %x", direct, request.Arguments)
	}

	if _, err := PrepareProcedureCallRequest(contract, "missing", []byte(`{}`)); !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("missing procedure error = %v, want ErrSurfaceNotFound", err)
	}
	if _, err := PrepareProcedureCallRequest(contract, "schema_less", []byte(`{}`)); !errors.Is(err, ErrArgumentSchemaMissing) {
		t.Fatalf("schema-less procedure error = %v, want ErrArgumentSchemaMissing", err)
	}
	if _, err := PrepareProcedureCallRequest(contract, "archive_message", []byte(`{"message_id":"wrong","archive":true}`)); !errors.Is(err, ErrInvalidArgumentJSON) {
		t.Fatalf("mismatched procedure argument error = %v, want ErrInvalidArgumentJSON", err)
	}
}

func TestProcedureResultSchemaRejectsMissingOrMismatchedContractShape(t *testing.T) {
	result := schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "archived", Type: "bool"},
		{Name: "revision", Type: "uint64"},
	}}
	contract := workflowContractFixture()
	contract.Procedures = []shunter.ProcedureDescription{
		{Name: "archive_message", Result: &result},
		{Name: "result_less"},
	}

	got, err := ProcedureResultSchema(contract, " archive_message ")
	if err != nil {
		t.Fatalf("ProcedureResultSchema: %v", err)
	}
	if len(got.Columns) != 2 || got.Columns[0].Name != "archived" || got.Columns[1].Type != "uint64" {
		t.Fatalf("procedure result schema = %+v, want declared archived/revision shape", got)
	}
	if _, err := ProcedureResultSchema(contract, "missing"); !errors.Is(err, ErrSurfaceNotFound) {
		t.Fatalf("missing procedure result error = %v, want ErrSurfaceNotFound", err)
	}
	if _, err := ProcedureResultSchema(contract, "result_less"); !errors.Is(err, ErrResultSchemaMissing) {
		t.Fatalf("schema-less procedure result error = %v, want ErrResultSchemaMissing", err)
	}
	if _, err := ProductValueToJSONRow(got, types.ProductValue{types.NewBool(true)}); !errors.Is(err, ErrProductValueShape) {
		t.Fatalf("mismatched declared procedure result shape error = %v, want ErrProductValueShape", err)
	}
}
