package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/contractworkflow"
	"github.com/ponchione/shunter/examples/hosted-chat/internal/app"
)

func main() {
	out := flag.String("out", "examples/hosted-chat/shunter.contract.json", "contract output path")
	flag.Parse()

	rt, err := shunter.Build(app.Module(), shunter.Config{DataDir: "./examples/hosted-chat/.contract-data"})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer rt.Close()

	if err := contractworkflow.ExportRuntimeFile(rt, *out); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
