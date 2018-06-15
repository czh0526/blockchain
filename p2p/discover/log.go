package discover

import (
	"github.com/czh0526/blockchain/log"
)

var mlog log.Logger

func init() {
	mlog = log.New()
}
