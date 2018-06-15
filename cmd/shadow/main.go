package main

import (
	"fmt"
	"os"

	"github.com/czh0526/blockchain/internal/debug"
	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/node"
	"gopkg.in/urfave/cli.v1"
)

const (
	clientIdentifier = "shadow"
)

var (
	app = cli.NewApp()
)

func init() {
	app.Action = shadow
	app.Flags = append(app.Flags, debug.Flags...)

	app.Before = func(ctx *cli.Context) error {
		if err := debug.Setup(ctx); err != nil {
			return err
		}
		return nil
	}
}

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func shadow(ctx *cli.Context) error {
	node, err := node.New()
	if err != nil {
		return err
	}

	startNode(ctx, node)
	node.Wait()
	return nil
}

func startNode(ctx *cli.Context, node *node.Node) {
	if err := node.Start(); err != nil {
		log.Error(fmt.Sprintf("Error starting protocol stack: %v", err))
		os.Exit(1)
	}
}
