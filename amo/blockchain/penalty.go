package blockchain

import (
	"encoding/hex"
	"fmt"
	"math/big"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/amolabs/amoabci/amo/store"
	"github.com/amolabs/amoabci/amo/types"
)

// Convicts consist of
// - Malicious Validator: M
// - Lazy Validator: L

func PenalizeConvicts(
	store *store.Store,
	logger log.Logger,

	evidences []abci.Evidence,
	lazyValidators []crypto.Address,

	weightValidator, weightDelegator float64,
	penaltyRatioM, penaltyRatioL float64,
) (bool, error) {
	doValUpdate := false

	// handle evidences
	for _, evidence := range evidences {
		validator := evidence.GetValidator().Address
		tmp, err := penalize(
			store, logger,
			weightValidator, weightDelegator,
			validator, penaltyRatioM, "Evidence Penalty",
		)
		doValUpdate = doValUpdate || tmp
		if err != nil {
			return doValUpdate, err
		}
	}

	// handle lazyValidators
	for _, lazyValidator := range lazyValidators {
		tmp, err := penalize(
			store, logger,
			weightValidator, weightDelegator,
			lazyValidator, penaltyRatioL, "Downtime Penalty",
		)
		doValUpdate = doValUpdate || tmp
		if err != nil {
			return doValUpdate, err
		}
	}

	return doValUpdate, nil
}

func penalize(
	store *store.Store,
	logger log.Logger,

	weightValidator, weightDelegator float64,
	validator crypto.Address,
	ratio float64,
	penaltyType string,
) (bool, error) {
	doValUpdate := false
	zeroAmount := new(types.Currency).Set(0)

	holder := store.GetHolderByValidator(validator, false)
	if holder == nil {
		return doValUpdate, fmt.Errorf("no holder for validator: %X", validator)
	}
	vs := store.GetStake(holder, false) // validator's stake
	if vs == nil {
		return doValUpdate, fmt.Errorf("no stake for holder: %X", holder)
	}

	ds := store.GetDelegatesByDelegatee(holder, false) // delegators' stake
	es := store.GetEffStake(holder, false)

	// itof
	vsf := new(big.Float).SetInt(&vs.Amount.Int)
	esf := new(big.Float).SetInt(&es.Amount.Int)

	prf := new(big.Float).SetFloat64(ratio)

	penalty := types.Currency{}
	pf := esf.Mul(esf, prf) // penalty = effStake * penaltyRatio
	pf.Int(&penalty.Int)

	// distribute penalty
	// TODO: unify this code snippet with those in incentive.go
	var (
		wsumf, wf   big.Float // weighted sum
		tmpf        big.Float // tmp
		tmpc, tmpc2 types.Currency
	)

	wf.SetFloat64(weightValidator)
	wsumf.Mul(&wf, vsf)
	wf.SetFloat64(weightDelegator)
	for _, d := range ds {
		df := new(big.Float).SetInt(&d.Amount.Int)
		tmpf.Mul(&wf, df)
		wsumf.Add(&wsumf, &tmpf)
	}

	// individual penalties for delegators
	// NOTE: merkle version equals to last height + 1, so until commit() merkle
	// version equals to the current height
	height := store.GetMerkleVersion()
	tmpc.Set(0) // subtotal for delegate holders
	for _, d := range ds {
		df := new(big.Float).SetInt(&d.Amount.Int)
		tmpc2 = *partialAmount(weightDelegator, df, &wsumf, &penalty)
		tmpc.Add(&tmpc2) // update subtotal

		if tmpc2.Equals(zeroAmount) {
			continue
		}
		// update stake
		d.Delegate.Amount.Sub(&tmpc2)
		if d.Delegate.Amount.LessThan(zeroAmount) { // XXX: is it necessary?
			d.Delegate.Amount.Set(0)
		}
		store.SetDelegate(d.Delegator, d.Delegate)
		// add history record
		store.AddPenaltyRecord(height, d.Delegator, &tmpc2)
		// log XXX: remove this?
		logger.Debug(penaltyType,
			"delegator", hex.EncodeToString(d.Delegator), "penalty", tmpc.String())
		doValUpdate = true
	}
	// calc voter(validator) penalty
	tmpc2.Int.Sub(&penalty.Int, &tmpc.Int)
	if tmpc2.Equals(zeroAmount) {
		return doValUpdate, nil
	}
	// update stake
	store.SlashStakes(holder, tmpc2, false)
	// add history record
	store.AddPenaltyRecord(height, holder, &tmpc2)
	// log XXX: remove this?
	logger.Debug(penaltyType,
		"validator", hex.EncodeToString(holder), "penalty", tmpc2.String())
	doValUpdate = true

	return doValUpdate, nil
}
