package debug

import (
	"io"
	"os"

	"github.com/czh0526/blockchain/log"
	"gopkg.in/urfave/cli.v1"
)

var (
	vmoduleFlag = cli.StringFlag{
		Name:  "vmodule",
		Usage: "每个模块的日志级别：逗号分开的配置列表(e.g. eth/*=5,p2p=4), 0=silent, 1=error, 2=warn, 3=info, 4=debug, 5=detail",
		Value: "",
	}
)

var Flags = []cli.Flag{
	vmoduleFlag,
}

var glogger *log.GlogHandler

func init() {
	output := io.Writer(os.Stderr)
	glogger = log.NewGlogHandler(log.StreamHandler(output, log.TerminalFormat(true)))
}

func Setup(ctx *cli.Context) error {
	// 设置 filter
	glogger.Vmodule(ctx.GlobalString(vmoduleFlag.Name))
	// 将 root logger 进行 glog 包装
	log.Root().SetHandler(glogger)

	return nil
}
