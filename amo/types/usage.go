package types

import (
	"encoding/json"
	"time"

	"github.com/tendermint/tendermint/crypto"
	cmn "github.com/tendermint/tendermint/libs/common"
)

type UsageValue struct {
	Custody cmn.HexBytes `json:"custody"`
	Exp     time.Time    `json:"exp"`

	Register json.RawMessage `json:"register"`
	Request  json.RawMessage `json:"request"`
	Grant    json.RawMessage `json:"grant"`
}

type UsageValueEx struct {
	*UsageValue
	Buyer crypto.Address `json:"buyer"`
}

func (value UsageValue) IsExpired() bool {
	return value.Exp.Before(time.Now())
}
