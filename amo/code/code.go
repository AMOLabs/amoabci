package code

const (
	TxCodeOK uint32 = iota
	TxCodeBadParam
	TxCodeTooOldTx
	TxCodeAlreadyProcessedTx
	TxCodeInvalidAmount
	TxCodeNotEnoughBalance
	TxCodeSelfTransaction
	TxCodePermissionDenied
	TxCodeAlreadyRegistered
	TxCodeAlreadyRequested
	TxCodeAlreadyGranted
	TxCodeParcelNotFound
	TxCodeRequestNotFound
	TxCodeUsageNotFound
	TxCodeBadSignature
	TxCodeMultipleDelegates
	TxCodeDelegateNotFound
	TxCodeNoStake
	TxCodeImproperStakingUnit
	TxCodeImproperStakeAmount
	TxCodeHeightTaken
	TxCodeBadValidator
	TxCodeLastValidator
	TxCodeDelegateExists
	TxCodeStakeLocked

	TxCodeImproperDraftID
	TxCodeImproperDraftDeposit
	TxCodeProposedDraft
	TxCodeDraftInProcess

	TxCodeUnknown
)

const (
	QueryCodeOK uint32 = iota
	QueryCodeBadPath
	QueryCodeNoKey
	QueryCodeBadKey
	QueryCodeNoMatch
)
