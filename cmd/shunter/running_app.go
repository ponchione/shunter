package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
)

const defaultRunningAppTimeout = 10 * time.Second

func runCall(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter call")
	flags := newRunningAppFlags(fs)
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := validateRunningAppCommon(stderr, flags); code != 0 {
		return code
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: flags.urlValue(),
			Code:      "invalid_arguments",
			Message:   "call requires reducer name and optional JSON arguments",
		})
		return 2
	}

	name := strings.TrimSpace(fs.Arg(0))
	rawArgs, err := flags.argumentBytes(fs.Args()[1:])
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}

	contract, err := readContractFile(flags.contractValue(), "call contract")
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      "contract_error",
			Message:   err.Error(),
		})
		return 1
	}
	request, err := prepareReducerCall(contract, name, rawArgs, flags.argsHexValue() != "")
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	token, err := flags.token()
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: flags.urlValue(),
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	target, err := normalizeRunningAppURL(flags.urlValue())
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: flags.urlValue(),
			Surface:   request.Name,
			Code:      "invalid_url",
			Message:   err.Error(),
		})
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), flags.timeoutValue())
	defer cancel()
	identity, update, err := protocolclient.DialAndCallReducer(ctx, protocolclient.Options{
		URL:            target,
		Token:          token,
		AllowAnonymous: flags.allowDevAnonymousValue(),
	}, protocolclient.ReducerCallRequest{
		Name:      request.Name,
		Arguments: request.Arguments,
	})
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "call",
			TargetURL: target,
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	if err := writeCallSuccess(stdout, flags.formatValue(), contract, target, identity, update); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	return 0
}

func runQuery(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter query")
	flags := newRunningAppFlags(fs)
	sqlText := fs.String("sql", "", "raw read-only SQL query to execute instead of a declared query name")
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := validateRunningAppCommon(stderr, flags); code != 0 {
		return code
	}
	if strings.TrimSpace(*sqlText) != "" {
		if fs.NArg() != 0 {
			writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
				Command:   "query",
				TargetURL: flags.urlValue(),
				Code:      "invalid_arguments",
				Message:   "query --sql does not accept positional query name or arguments",
			})
			return 2
		}
		return runSQLQuery(stdout, stderr, flags, strings.TrimSpace(*sqlText))
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Code:      "invalid_arguments",
			Message:   "query requires query name and optional JSON arguments",
		})
		return 2
	}

	name := strings.TrimSpace(fs.Arg(0))
	rawArgs, hasArgs, err := flags.optionalArgumentBytes(fs.Args()[1:])
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}

	contract, err := readContractFile(flags.contractValue(), "query contract")
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      "contract_error",
			Message:   err.Error(),
		})
		return 1
	}
	request, err := prepareDeclaredQuery(contract, name, rawArgs, hasArgs, flags.argsHexValue() != "")
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	token, err := flags.token()
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	target, err := normalizeRunningAppURL(flags.urlValue())
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   request.Name,
			Code:      "invalid_url",
			Message:   err.Error(),
		})
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), flags.timeoutValue())
	defer cancel()
	identity, response, err := protocolclient.DialAndExecuteDeclaredQuery(ctx, protocolclient.Options{
		URL:            target,
		Token:          token,
		AllowAnonymous: flags.allowDevAnonymousValue(),
	}, protocolclient.DeclaredQueryRequest{
		Name:          request.Name,
		Parameters:    request.Parameters,
		HasParameters: request.HasParameters,
	})
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: target,
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	if err := writeQuerySuccess(stdout, flags.formatValue(), contract, target, identity, response, request.Name); err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: target,
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	return 0
}

func runSQLQuery(stdout, stderr io.Writer, flags runningAppFlags, sqlText string) int {
	contract, err := readContractFile(flags.contractValue(), "query contract")
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   sqlText,
			Code:      "contract_error",
			Message:   err.Error(),
		})
		return 1
	}
	token, err := flags.token()
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   sqlText,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	target, err := normalizeRunningAppURL(flags.urlValue())
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: flags.urlValue(),
			Surface:   sqlText,
			Code:      "invalid_url",
			Message:   err.Error(),
		})
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), flags.timeoutValue())
	defer cancel()
	identity, response, err := protocolclient.DialAndExecuteSQLQuery(ctx, protocolclient.Options{
		URL:            target,
		Token:          token,
		AllowAnonymous: flags.allowDevAnonymousValue(),
	}, protocolclient.SQLQueryRequest{
		QueryString: sqlText,
	})
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: target,
			Surface:   sqlText,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	if err := writeSQLQuerySuccess(stdout, flags.formatValue(), contract, target, identity, response, sqlText); err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "query",
			TargetURL: target,
			Surface:   sqlText,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	return 0
}

func runProcedure(stdout, stderr io.Writer, args []string) int {
	fs := newFlagSet(stderr, "shunter procedure")
	flags := newRunningAppFlags(fs)
	if code, stop := parseFlags(fs, args); stop {
		return code
	}
	if code := validateRunningAppCommon(stderr, flags); code != 0 {
		return code
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: flags.urlValue(),
			Code:      "invalid_arguments",
			Message:   "procedure requires procedure name and optional JSON arguments",
		})
		return 2
	}
	name := strings.TrimSpace(fs.Arg(0))
	rawArgs, err := flags.argumentBytes(fs.Args()[1:])
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	contract, err := readContractFile(flags.contractValue(), "procedure contract")
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      "contract_error",
			Message:   err.Error(),
		})
		return 1
	}
	request, err := prepareProcedureCall(contract, name, rawArgs, flags.argsHexValue() != "")
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: flags.urlValue(),
			Surface:   name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	token, err := flags.token()
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: flags.urlValue(),
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 2
	}
	target, err := normalizeRunningAppURL(flags.urlValue())
	if err != nil {
		writeRunningAppUsageError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: flags.urlValue(),
			Surface:   request.Name,
			Code:      "invalid_url",
			Message:   err.Error(),
		})
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), flags.timeoutValue())
	defer cancel()
	identity, response, err := protocolclient.DialAndCallProcedure(ctx, protocolclient.Options{
		URL:            target,
		Token:          token,
		AllowAnonymous: flags.allowDevAnonymousValue(),
	}, protocolclient.ProcedureCallRequest{
		Name:      request.Name,
		Arguments: request.Arguments,
	})
	if err != nil {
		writeRunningAppRuntimeError(stderr, flags.formatValue(), runningAppError{
			Command:   "procedure",
			TargetURL: target,
			Surface:   request.Name,
			Code:      classifyRunningAppErrorCode(err),
			Message:   err.Error(),
		})
		return 1
	}
	if err := writeProcedureSuccess(stdout, flags.formatValue(), contract, target, identity, response, request.Name); err != nil {
		writeCLIError(stderr, err)
		return 1
	}
	return 0
}

type runningAppFlags struct {
	url               *string
	contract          *string
	tokenFlag         *string
	tokenFile         *string
	timeout           *time.Duration
	format            *string
	args              *string
	argsFile          *string
	argsHex           *string
	allowDevAnonymous *bool
}

func newRunningAppFlags(fs interface {
	String(string, string, string) *string
	Duration(string, time.Duration, string) *time.Duration
	Bool(string, bool, string) *bool
}) runningAppFlags {
	return runningAppFlags{
		url:               fs.String("url", "", "running Shunter app URL; http(s) URLs are mapped to /subscribe WebSocket URLs"),
		contract:          fs.String("contract", "", "local contract JSON path"),
		tokenFlag:         fs.String("token", "", "bearer token for the running app"),
		tokenFile:         fs.String("token-file", "", "file containing bearer token"),
		timeout:           fs.Duration("timeout", defaultRunningAppTimeout, "bounded end-to-end timeout"),
		format:            fs.String("format", contractworkflow.FormatText, "output format: text or json"),
		args:              fs.String("args", "", "JSON object arguments"),
		argsFile:          fs.String("args-file", "", "file containing JSON object arguments"),
		argsHex:           fs.String("args-hex", "", "pre-encoded BSATN argument bytes as hex"),
		allowDevAnonymous: fs.Bool("allow-dev-anonymous", false, "allow an explicit tokenless connection to a development anonymous-auth app"),
	}
}

func validateRunningAppCommon(stderr io.Writer, flags runningAppFlags) int {
	if code := requirePath(stderr, "url", flags.urlValue()); code != 0 {
		return code
	}
	if code := requirePath(stderr, "contract", flags.contractValue()); code != 0 {
		return code
	}
	if err := contractworkflow.ValidateFormat(flags.formatValue()); err != nil {
		writeCLIError(stderr, err)
		return 2
	}
	if flags.timeoutValue() <= 0 {
		writeCLIErrorf(stderr, "--timeout must be positive\n")
		return 2
	}
	return 0
}

func (f runningAppFlags) urlValue() string {
	if f.url == nil {
		return ""
	}
	return strings.TrimSpace(*f.url)
}

func (f runningAppFlags) contractValue() string {
	if f.contract == nil {
		return ""
	}
	return strings.TrimSpace(*f.contract)
}

func (f runningAppFlags) formatValue() string {
	if f.format == nil {
		return contractworkflow.FormatText
	}
	return strings.TrimSpace(*f.format)
}

func (f runningAppFlags) argsHexValue() string {
	if f.argsHex == nil {
		return ""
	}
	return strings.TrimSpace(*f.argsHex)
}

func (f runningAppFlags) timeoutValue() time.Duration {
	if f.timeout == nil {
		return defaultRunningAppTimeout
	}
	return *f.timeout
}

func (f runningAppFlags) token() (string, error) {
	if f.tokenFlag != nil && strings.TrimSpace(*f.tokenFlag) != "" {
		return strings.TrimSpace(*f.tokenFlag), nil
	}
	if f.tokenFile != nil && strings.TrimSpace(*f.tokenFile) != "" {
		data, err := os.ReadFile(strings.TrimSpace(*f.tokenFile))
		if err != nil {
			return "", fmt.Errorf("read token file %q: %w", strings.TrimSpace(*f.tokenFile), err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", errRunningAppMissingToken
		}
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("SHUNTER_TOKEN")); token != "" {
		return token, nil
	}
	if f.allowDevAnonymousValue() {
		return "", nil
	}
	return "", errRunningAppMissingToken
}

func (f runningAppFlags) allowDevAnonymousValue() bool {
	if f.allowDevAnonymous == nil {
		return false
	}
	return *f.allowDevAnonymous
}

func (f runningAppFlags) argumentBytes(positionals []string) ([]byte, error) {
	data, hasData, err := f.optionalArgumentBytes(positionals)
	if err != nil {
		return nil, err
	}
	if !hasData {
		return nil, fmt.Errorf("%w: JSON arguments are required", errRunningAppInvalidArguments)
	}
	return data, nil
}

func (f runningAppFlags) optionalArgumentBytes(positionals []string) ([]byte, bool, error) {
	sources := 0
	var data []byte
	var err error
	if f.args != nil && strings.TrimSpace(*f.args) != "" {
		sources++
		data = []byte(*f.args)
	}
	if f.argsFile != nil && strings.TrimSpace(*f.argsFile) != "" {
		sources++
		data, err = os.ReadFile(strings.TrimSpace(*f.argsFile))
		if err != nil {
			return nil, false, fmt.Errorf("read args file %q: %w", strings.TrimSpace(*f.argsFile), err)
		}
	}
	if f.argsHex != nil && strings.TrimSpace(*f.argsHex) != "" {
		sources++
		data, err = hex.DecodeString(strings.TrimSpace(*f.argsHex))
		if err != nil {
			return nil, false, fmt.Errorf("%w: args-hex: %v", errRunningAppInvalidArguments, err)
		}
	}
	if len(positionals) > 0 {
		sources++
		data = []byte(positionals[0])
	}
	if sources > 1 {
		return nil, false, fmt.Errorf("%w: provide only one of positional JSON, --args, --args-file, or --args-hex", errRunningAppInvalidArguments)
	}
	return data, sources == 1, nil
}

func prepareReducerCall(contract shunter.ModuleContract, name string, data []byte, rawHex bool) (contractworkflow.ReducerCallRequest, error) {
	reducer, ok := contractworkflow.FindReducer(contract, name)
	if !ok {
		return contractworkflow.ReducerCallRequest{}, fmt.Errorf("%w: reducer %q", contractworkflow.ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	if rawHex {
		return contractworkflow.ReducerCallRequest{Name: reducer.Name, Arguments: data}, nil
	}
	return contractworkflow.PrepareReducerCallRequest(contract, name, data)
}

func prepareProcedureCall(contract shunter.ModuleContract, name string, data []byte, rawHex bool) (contractworkflow.ProcedureCallRequest, error) {
	procedure, ok := contractworkflow.FindProcedure(contract, name)
	if !ok {
		return contractworkflow.ProcedureCallRequest{}, fmt.Errorf("%w: procedure %q", contractworkflow.ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	if rawHex {
		return contractworkflow.ProcedureCallRequest{Name: procedure.Name, Arguments: data}, nil
	}
	return contractworkflow.PrepareProcedureCallRequest(contract, name, data)
}

func prepareDeclaredQuery(contract shunter.ModuleContract, name string, data []byte, hasData, rawHex bool) (contractworkflow.DeclaredQueryRequest, error) {
	query, ok := contractworkflow.FindQuery(contract, name)
	if !ok {
		return contractworkflow.DeclaredQueryRequest{}, fmt.Errorf("%w: query %q", contractworkflow.ErrSurfaceNotFound, strings.TrimSpace(name))
	}
	if rawHex {
		if query.Parameters == nil || len(query.Parameters.Columns) == 0 {
			return contractworkflow.DeclaredQueryRequest{}, fmt.Errorf("%w: query %q does not accept arguments", contractworkflow.ErrInvalidArgumentJSON, query.Name)
		}
		return contractworkflow.DeclaredQueryRequest{Name: query.Name, Parameters: data, HasParameters: true}, nil
	}
	return contractworkflow.PrepareDeclaredQueryRequest(contract, name, data, hasData)
}

func normalizeRunningAppURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", protocolclient.ErrURLRequired
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("URL host is required")
	}
	switch cleanPath := path.Clean(parsed.Path); cleanPath {
	case ".", "/":
		parsed.Path = "/subscribe"
	default:
		if strings.HasSuffix(cleanPath, "/subscribe") {
			parsed.Path = cleanPath
		} else {
			parsed.Path = path.Join(cleanPath, "/subscribe")
		}
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

type runningAppError struct {
	Status    string `json:"status"`
	Scope     string `json:"scope"`
	Command   string `json:"command"`
	TargetURL string `json:"target_url"`
	Surface   string `json:"surface,omitempty"`
	Code      string `json:"error_code"`
	Message   string `json:"message"`
}

var (
	errRunningAppMissingToken      = errors.New("token is required for running-app admin commands")
	errRunningAppInvalidArguments  = errors.New("invalid running-app arguments")
	errRunningAppUnsupportedStatus = errors.New("unsupported reducer status")
)

func writeRunningAppUsageError(stderr io.Writer, format string, err runningAppError) {
	writeRunningAppError(stderr, format, err)
}

func writeRunningAppRuntimeError(stderr io.Writer, format string, err runningAppError) {
	writeRunningAppError(stderr, format, err)
}

func writeRunningAppError(stderr io.Writer, format string, err runningAppError) {
	err.Status = "error"
	err.Scope = "running_app"
	if strings.EqualFold(strings.TrimSpace(format), contractworkflow.FormatJSON) {
		data, marshalErr := marshalIndentedJSON(err)
		if marshalErr == nil {
			_, _ = stderr.Write(data)
			return
		}
	}
	writeCLIErrorf(stderr, "%s\n", err.Message)
}

func classifyRunningAppErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errRunningAppMissingToken), errors.Is(err, protocolclient.ErrTokenRequired):
		return "missing_token"
	case errors.Is(err, protocolclient.ErrURLRequired):
		return "invalid_url"
	case errors.Is(err, protocolclient.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, protocolclient.ErrReducerFailed):
		return "reducer_error"
	case errors.Is(err, protocolclient.ErrDeclaredQueryFailed), errors.Is(err, protocolclient.ErrSQLQueryFailed):
		return "query_error"
	case errors.Is(err, protocolclient.ErrProcedureFailed):
		return "procedure_error"
	case errors.Is(err, protocolclient.ErrProtocolVersion), errors.Is(err, protocolclient.ErrUnexpectedMessage), errors.Is(err, protocolclient.ErrNonBinaryMessage), errors.Is(err, protocolclient.ErrResponseMismatch):
		return "protocol_error"
	case isDiagnosticsHTTPStatusError(err):
		return "http_status"
	case errors.Is(err, contractworkflow.ErrSurfaceNotFound):
		return "unknown_surface"
	case errors.Is(err, contractworkflow.ErrInvalidArgumentJSON), errors.Is(err, contractworkflow.ErrArgumentSchemaMissing), errors.Is(err, errRunningAppInvalidArguments):
		return "invalid_arguments"
	case errors.Is(err, contractworkflow.ErrResultSchemaMissing), errors.Is(err, contractworkflow.ErrResultTableMismatch), errors.Is(err, contractworkflow.ErrResultTableCount), errors.Is(err, contractworkflow.ErrProductValueShape):
		return "decode_error"
	default:
		return "runtime_error"
	}
}

type callSuccess struct {
	Status       string `json:"status"`
	Scope        string `json:"scope"`
	Command      string `json:"command"`
	TargetURL    string `json:"target_url"`
	Module       string `json:"module"`
	Surface      string `json:"surface"`
	Identity     string `json:"identity"`
	ConnectionID string `json:"connection_id"`
	TxStatus     string `json:"tx_status"`
	DurationUS   int64  `json:"duration_micros"`
}

func writeCallSuccess(stdout io.Writer, format string, contract shunter.ModuleContract, target string, identity protocol.IdentityToken, update protocol.TransactionUpdate) error {
	status := "committed"
	switch update.Status.(type) {
	case protocol.StatusCommitted:
	case protocol.StatusFailed:
		status = "failed"
	default:
		return fmt.Errorf("%w: %T", errRunningAppUnsupportedStatus, update.Status)
	}
	out := callSuccess{
		Status:       "ok",
		Scope:        "running_app",
		Command:      "call",
		TargetURL:    target,
		Module:       contract.Module.Name,
		Surface:      update.ReducerCall.ReducerName,
		Identity:     hex.EncodeToString(identity.Identity[:]),
		ConnectionID: hex.EncodeToString(identity.ConnectionID[:]),
		TxStatus:     status,
		DurationUS:   update.TotalHostExecutionDuration,
	}
	if strings.EqualFold(strings.TrimSpace(format), contractworkflow.FormatJSON) {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "Status: ok\nScope: running_app\nCommand: call\nTarget: %s\nModule: %s\nReducer: %s\nTransaction: %s\n", out.TargetURL, out.Module, out.Surface, out.TxStatus)
	return nil
}

type querySuccess struct {
	Status       string                         `json:"status"`
	Scope        string                         `json:"scope"`
	Command      string                         `json:"command"`
	TargetURL    string                         `json:"target_url"`
	Module       string                         `json:"module"`
	Surface      string                         `json:"surface"`
	Identity     string                         `json:"identity"`
	ConnectionID string                         `json:"connection_id"`
	Result       contractworkflow.JSONQueryRows `json:"result"`
	DurationUS   int64                          `json:"duration_micros"`
}

type procedureSuccess struct {
	Status       string `json:"status"`
	Scope        string `json:"scope"`
	Command      string `json:"command"`
	TargetURL    string `json:"target_url"`
	Module       string `json:"module"`
	Surface      string `json:"surface"`
	Identity     string `json:"identity"`
	ConnectionID string `json:"connection_id"`
	ResultHex    string `json:"result_hex"`
	DurationUS   int64  `json:"duration_micros"`
}

func writeProcedureSuccess(stdout io.Writer, format string, contract shunter.ModuleContract, target string, identity protocol.IdentityToken, response protocol.ProcedureResponse, name string) error {
	out := procedureSuccess{
		Status:       "ok",
		Scope:        "running_app",
		Command:      "procedure",
		TargetURL:    target,
		Module:       contract.Module.Name,
		Surface:      name,
		Identity:     hex.EncodeToString(identity.Identity[:]),
		ConnectionID: hex.EncodeToString(identity.ConnectionID[:]),
		ResultHex:    hex.EncodeToString(response.Result),
		DurationUS:   response.TotalHostExecutionDuration,
	}
	if strings.EqualFold(strings.TrimSpace(format), contractworkflow.FormatJSON) {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "Status: ok\nScope: running_app\nCommand: procedure\nTarget: %s\nModule: %s\nProcedure: %s\nResult bytes: %d\n", out.TargetURL, out.Module, out.Surface, len(response.Result))
	return nil
}

func writeQuerySuccess(stdout io.Writer, format string, contract shunter.ModuleContract, target string, identity protocol.IdentityToken, response protocol.OneOffQueryResponse, name string) error {
	rows, err := contractworkflow.DecodeQueryResponseJSONResult(contract, name, response)
	if err != nil {
		return err
	}
	out := querySuccess{
		Status:       "ok",
		Scope:        "running_app",
		Command:      "query",
		TargetURL:    target,
		Module:       contract.Module.Name,
		Surface:      name,
		Identity:     hex.EncodeToString(identity.Identity[:]),
		ConnectionID: hex.EncodeToString(identity.ConnectionID[:]),
		Result:       rows,
		DurationUS:   response.TotalHostExecutionDuration,
	}
	if strings.EqualFold(strings.TrimSpace(format), contractworkflow.FormatJSON) {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "Status: ok\nScope: running_app\nCommand: query\nTarget: %s\nModule: %s\nQuery: %s\nRows: %d\n", out.TargetURL, out.Module, out.Surface, len(out.Result.Rows))
	return nil
}

func writeSQLQuerySuccess(stdout io.Writer, format string, contract shunter.ModuleContract, target string, identity protocol.IdentityToken, response protocol.OneOffQueryResponse, sqlText string) error {
	rows, err := contractworkflow.DecodeSQLQueryResponseJSONResult(contract, sqlText, response)
	if err != nil {
		return err
	}
	out := querySuccess{
		Status:       "ok",
		Scope:        "running_app",
		Command:      "query",
		TargetURL:    target,
		Module:       contract.Module.Name,
		Surface:      sqlText,
		Identity:     hex.EncodeToString(identity.Identity[:]),
		ConnectionID: hex.EncodeToString(identity.ConnectionID[:]),
		Result:       rows,
		DurationUS:   response.TotalHostExecutionDuration,
	}
	if strings.EqualFold(strings.TrimSpace(format), contractworkflow.FormatJSON) {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "Status: ok\nScope: running_app\nCommand: query\nTarget: %s\nModule: %s\nSQL: %s\nRows: %d\n", out.TargetURL, out.Module, out.Surface, len(out.Result.Rows))
	return nil
}

func writeJSON(w io.Writer, value any) error {
	data, err := marshalIndentedJSON(value)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
