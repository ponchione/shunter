// hosted-chat-json-assert validates the semantic JSON emitted by the hosted
// chat black-box gate. It intentionally ignores volatile identity, URL, and
// duration fields while checking exact command metadata and result rows.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
)

type commandReport struct {
	Status  string `json:"status"`
	Scope   string `json:"scope"`
	Command string `json:"command"`
	Module  string `json:"module"`
	Surface string `json:"surface"`
}

type messageRow struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

type queryResult struct {
	Name      string       `json:"name"`
	TableName string       `json:"table_name"`
	Rows      []messageRow `json:"rows"`
}

type queryReport struct {
	commandReport
	Result queryResult `json:"result"`
}

type rowFlags []string

func (f *rowFlags) String() string { return strings.Join(*f, ",") }

func (f *rowFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		exitError(errors.New("usage: hosted-chat-json-assert <command|query|equivalent> [flags]"))
	}
	var err error
	switch os.Args[1] {
	case "command":
		err = runCommand(os.Args[2:])
	case "query":
		err = runQuery(os.Args[2:])
	case "equivalent":
		err = runEquivalent(os.Args[2:])
	default:
		err = fmt.Errorf("unknown assertion mode %q", os.Args[1])
	}
	if err != nil {
		exitError(err)
	}
}

func runCommand(args []string) error {
	flags := flag.NewFlagSet("command", flag.ContinueOnError)
	file := flags.String("file", "", "JSON report path")
	command := flags.String("command", "", "expected command")
	surface := flags.String("surface", "", "expected surface")
	if err := flags.Parse(args); err != nil {
		return err
	}
	data, err := readRequiredFile(*file)
	if err != nil {
		return err
	}
	return assertCommand(data, *command, *surface)
}

func runQuery(args []string) error {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	file := flags.String("file", "", "JSON report path")
	surface := flags.String("surface", "", "expected surface")
	resultName := flags.String("result-name", "", "expected result name")
	table := flags.String("table", "", "expected table")
	var rows rowFlags
	flags.Var(&rows, "row", "expected id|author|body row, in result order")
	if err := flags.Parse(args); err != nil {
		return err
	}
	wantRows := make([]messageRow, len(rows))
	for i, row := range rows {
		parts := strings.SplitN(row, "|", 3)
		if len(parts) != 3 {
			return fmt.Errorf("--row %d = %q, want id|author|body", i, row)
		}
		wantRows[i] = messageRow{ID: parts[0], Author: parts[1], Body: parts[2]}
	}
	data, err := readRequiredFile(*file)
	if err != nil {
		return err
	}
	return assertQuery(data, *surface, *resultName, *table, wantRows)
}

func runEquivalent(args []string) error {
	flags := flag.NewFlagSet("equivalent", flag.ContinueOnError)
	before := flags.String("before", "", "JSON report before backup")
	after := flags.String("after", "", "JSON report after restore")
	if err := flags.Parse(args); err != nil {
		return err
	}
	beforeData, err := readRequiredFile(*before)
	if err != nil {
		return err
	}
	afterData, err := readRequiredFile(*after)
	if err != nil {
		return err
	}
	return assertQueryEquivalent(beforeData, afterData)
}

func assertCommand(data []byte, wantCommand, wantSurface string) error {
	var got commandReport
	if err := decodeOneJSON(data, &got); err != nil {
		return err
	}
	for _, field := range []struct {
		path string
		got  string
		want string
	}{
		{path: "status", got: got.Status, want: "ok"},
		{path: "scope", got: got.Scope, want: "running_app"},
		{path: "command", got: got.Command, want: wantCommand},
		{path: "module", got: got.Module, want: "hosted_chat"},
		{path: "surface", got: got.Surface, want: wantSurface},
	} {
		if field.got != field.want {
			return fmt.Errorf("%s = %q, want %q", field.path, field.got, field.want)
		}
	}
	return nil
}

func assertQuery(data []byte, wantSurface, wantName, wantTable string, wantRows []messageRow) error {
	var got queryReport
	if err := decodeOneJSON(data, &got); err != nil {
		return err
	}
	if err := assertCommand(data, "query", wantSurface); err != nil {
		return err
	}
	if got.Result.Name != wantName {
		return fmt.Errorf("result.name = %q, want %q", got.Result.Name, wantName)
	}
	if got.Result.TableName != wantTable {
		return fmt.Errorf("result.table_name = %q, want %q", got.Result.TableName, wantTable)
	}
	if len(got.Result.Rows) != len(wantRows) {
		return fmt.Errorf("result.rows count = %d, want %d; observed=%+v", len(got.Result.Rows), len(wantRows), got.Result.Rows)
	}
	for i := range wantRows {
		if got.Result.Rows[i] != wantRows[i] {
			return fmt.Errorf("result.rows[%d] = %+v, want %+v", i, got.Result.Rows[i], wantRows[i])
		}
	}
	return nil
}

func assertQueryEquivalent(beforeData, afterData []byte) error {
	var before, after queryReport
	if err := decodeOneJSON(beforeData, &before); err != nil {
		return fmt.Errorf("before restore: %w", err)
	}
	if err := decodeOneJSON(afterData, &after); err != nil {
		return fmt.Errorf("after restore: %w", err)
	}
	if before.commandReport != after.commandReport {
		return fmt.Errorf("command metadata after restore = %+v, want %+v", after.commandReport, before.commandReport)
	}
	if !reflect.DeepEqual(before.Result, after.Result) {
		return fmt.Errorf("result after restore = %+v, want %+v", after.Result, before.Result)
	}
	return nil
}

func decodeOneJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("JSON report contains more than one value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func readRequiredFile(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("--file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func exitError(err error) {
	fmt.Fprintf(os.Stderr, "hosted-chat JSON assertion failed: %v\n", err)
	os.Exit(1)
}
