package operation

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"github.com/amolabs/amoabci/amo/db"
	"github.com/amolabs/tendermint-amo/crypto"
	"github.com/amolabs/tendermint-amo/crypto/p256"
	cmn "github.com/amolabs/tendermint-amo/libs/common"
	"strings"
)

const (
	TxTransfer = "transfer"
	TxRegister = "register"
	TxRequest  = "request"
	TxCancel   = "cancel"
	TxGrant    = "grant"
	TxRevoke   = "revoke"
	TxDiscard  = "discard"
)

type Message struct {
	Command       string          `json:"command"`
	Signer        crypto.Address  `json:"signer"`
	SigningPubKey p256.PubKeyP256 `json:"singingPubKey"`
	Signature     cmn.HexBytes    `json:"signature"`
	Payload       json.RawMessage `json:"payload"`
	Nonce         uint32          `json:"nonce"`
}

func (m *Message) GetSigningBytes() []byte {
	var buf bytes.Buffer
	// Command|Signer|SigningPubKey|Payload|Nonce
	buf.Write([]byte(m.Command))
	buf.Write(m.Signer)
	buf.Write(m.SigningPubKey[:])
	buf.Write(m.Payload)
	nonce := make([]byte, 32/8)
	binary.LittleEndian.PutUint32(nonce, m.Nonce)
	buf.Write(nonce)
	return buf.Bytes()
}

func (m *Message) Sign(privKey crypto.PrivKey) error {
	pubKey := privKey.PubKey()
	m.Nonce = cmn.RandUint32()
	m.Signer = pubKey.Address()
	copy(m.SigningPubKey[:], pubKey.Bytes())
	sb := m.GetSigningBytes()
	sig, err := privKey.Sign(sb)
	if err != nil {
		return err
	}
	m.Signature = sig
	return nil
}

func (m *Message) Verify() bool {
	sb := m.GetSigningBytes()
	return m.SigningPubKey.VerifyBytes(sb, m.Signature)
}

type Operation interface {
	Check(store *db.Store, signer crypto.Address) uint32
	Execute(store *db.Store, signer crypto.Address) (uint32, []cmn.KVPair)
}

func ParseTx(tx []byte) (Message, Operation) {
	var message Message

	err := json.Unmarshal(tx, &message)
	if err != nil {
		panic(err)
	}

	message.Command = strings.ToLower(message.Command)
	var payload interface{}
	switch message.Command {
	case TxTransfer:
		payload = new(Transfer)
	case TxRegister:
		payload = new(Register)
	case TxRequest:
		payload = new(Request)
	case TxCancel:
		payload = new(Cancel)
	case TxGrant:
		payload = new(Grant)
	case TxRevoke:
		payload = new(Revoke)
	case TxDiscard:
		payload = new(Discard)
	default:
		panic(cmn.NewError("Invalid operation command: %v", message.Command))
	}

	err = json.Unmarshal(message.Payload, &payload)
	if err != nil {
		panic(err)
	}

	return message, payload.(Operation)
}