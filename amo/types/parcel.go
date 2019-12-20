package types

import (
	"encoding/json"

	"github.com/tendermint/tendermint/crypto"
	cmn "github.com/tendermint/tendermint/libs/common"
)

type ParcelValue struct {
	Owner        crypto.Address `json:"owner"`
	Custody      cmn.HexBytes   `json:"custody"`
	Info         cmn.HexBytes   `json:"info,omitempty"`
	ProxyAccount crypto.Address `json:"proxy_account,omitempty"`

	Register json.RawMessage `json:"register"`
}

type ParcelValueEx struct {
	*ParcelValue
	Requests []*RequestValueEx `json:"requests,omitempty"`
	Usages   []*UsageValueEx   `json:"usages,omitempty"`
}
