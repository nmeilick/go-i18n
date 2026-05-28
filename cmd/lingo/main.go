package main

import (
	"os"

	"github.com/nmeilick/go-i18n/cmd/lingo/internal/cli"
	_ "golang.org/x/crypto/x509roots/fallback"
	_ "time/tzdata"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
