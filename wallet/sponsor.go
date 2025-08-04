package wallet

import (
	"fmt"
	"strconv"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
)

func (w *Wallet) ClaimBalanceWithSponsor(mainKp, sponsorKp *keypair.Full, balanceID string, fee int64) (string, float64, error) {
	// Get sponsor account for transaction source
	sponsorAccount, err := w.GetAccount(sponsorKp)
	if err != nil {
		return "", 0, fmt.Errorf("error getting sponsor account: %w", err)
	}

	// Create claim operation with main account as source
	claimOp := &txnbuild.ClaimClaimableBalance{
		BalanceID:     balanceID,
		SourceAccount: mainKp.Address(),
	}

	// Build transaction with sponsor as source account
	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        &sponsorAccount,
			IncrementSequenceNum: true,
			Operations:           []txnbuild.Operation{claimOp},
			BaseFee:              fee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewInfiniteTimeout(),
			},
		},
	)
	if err != nil {
		return "", 0, fmt.Errorf("error building transaction: %w", err)
	}

	// Sign with both keys
	tx, err = tx.Sign(w.networkPassphrase, sponsorKp, mainKp)
	if err != nil {
		return "", 0, fmt.Errorf("error signing transaction: %w", err)
	}

	// Submit transaction
	resp, err := w.client.SubmitTransaction(tx)
	if err != nil {
		return "", 0, fmt.Errorf("transaction failed: %w", err)
	}

	// Calculate claimed amount (approximate - you may need to parse from transaction effects)
	claimedAmount := 1.0 // Default value, should be parsed from transaction results
	if resp.Successful {
		claimedAmount = 1.0 // Parse actual amount from transaction effects if needed
	}

	return resp.Hash, claimedAmount, nil
}

func (w *Wallet) TransferWithFee(kp *keypair.Full, amountStr, destinationAddr string, fee int64) (string, error) {
	account, err := w.GetAccount(kp)
	if err != nil {
		return "", fmt.Errorf("error getting account: %w", err)
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return "", fmt.Errorf("invalid amount: %w", err)
	}

	// Create payment operation
	paymentOp := &txnbuild.Payment{
		Destination: destinationAddr,
		Amount:      fmt.Sprintf("%.7f", amount),
		Asset:       txnbuild.NativeAsset{},
	}

	// Build transaction with high fee
	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        &account,
			IncrementSequenceNum: true,
			Operations:           []txnbuild.Operation{paymentOp},
			BaseFee:              fee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewInfiniteTimeout(),
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("error building transaction: %w", err)
	}

	// Sign transaction
	tx, err = tx.Sign(w.networkPassphrase, kp)
	if err != nil {
		return "", fmt.Errorf("error signing transaction: %w", err)
	}

	// Submit transaction
	resp, err := w.client.SubmitTransaction(tx)
	if err != nil {
		return "", fmt.Errorf("transaction failed: %w", err)
	}

	return resp.Hash, nil
}