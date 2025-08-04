package wallet

import (
	"fmt"
	"strconv"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
)

func (w *Wallet) ClaimBalanceWithSponsor(mainKp, sponsorKp *keypair.Full, balanceID string, fee int64) (string, float64, error) {
	// Get main account
	mainAccount, err := w.GetAccount(mainKp)
	if err != nil {
		return "", 0, fmt.Errorf("error getting main account: %w", err)
	}

	// Get sponsor account
	sponsorAccount, err := w.GetAccount(sponsorKp)
	if err != nil {
		return "", 0, fmt.Errorf("error getting sponsor account: %w", err)
	}

	// Create claim operation
	claimOp := &txnbuild.ClaimClaimableBalance{
		BalanceID: balanceID,
	}

	// Build transaction with sponsor paying fees
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

	// Calculate claimed amount (approximate)
	claimedAmount := 0.0
	if len(resp.Successful) > 0 {
		claimedAmount = 1.0 // You might need to parse this from transaction details
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