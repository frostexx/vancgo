package server

import (
	"context"
	"fmt"
	"pi/util"
	"pi/wallet"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/protocols/horizon"
)

type ConcurrentBot struct {
	wallet            *wallet.Wallet
	conn              *websocket.Conn
	mainKp            *keypair.Full
	sponsorKp         *keypair.Full
	withdrawalAddress string
	amount            string
	lockedBalanceID   string
	mutex             sync.RWMutex
	ctx               context.Context
	cancel            context.CancelFunc
}

func NewConcurrentBot(w *wallet.Wallet, conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, withdrawalAddr, amount, lockedBalanceID string) *ConcurrentBot {
	ctx, cancel := context.WithCancel(context.Background())
	return &ConcurrentBot{
		wallet:            w,
		conn:              conn,
		mainKp:            mainKp,
		sponsorKp:         sponsorKp,
		withdrawalAddress: withdrawalAddr,
		amount:            amount,
		lockedBalanceID:   lockedBalanceID,
		ctx:               ctx,
		cancel:            cancel,
	}
}

func (cb *ConcurrentBot) StartAggressiveBot(balance *horizon.ClaimableBalance, unlockTime time.Time) {
	cb.sendMessage("🚀 Starting at exact unlock time")

	// Start concurrent operations immediately at unlock time
	var wg sync.WaitGroup
	
	// Network flooding goroutines (5 concurrent claim attempts)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cb.aggressiveClaim(id, balance)
		}(i)
	}

	// Start transfer monitoring at exact unlock time (with 0 balance)
	wg.Add(1)
	go func() {
		defer wg.Done()
		cb.continuousTransferMonitor()
	}()

	wg.Wait()
}

func (cb *ConcurrentBot) aggressiveClaim(goroutineID int, balance *horizon.ClaimableBalance) {
	maxAttempts := 200
	
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-cb.ctx.Done():
			return
		default:
		}

		var hash string
		var amount float64
		var err error

		// Use sponsor if available, otherwise use main wallet
		if cb.sponsorKp != nil {
			// Use high competitive fees with sponsor
			fee := util.GetCompetitiveFee()
			hash, amount, err = cb.wallet.ClaimBalanceWithSponsor(cb.mainKp, cb.sponsorKp, cb.lockedBalanceID, fee)
		} else {
			// Use main wallet with competitive fee
			hash, amount, err = cb.wallet.WithdrawClaimableBalance(cb.mainKp, cb.amount, cb.lockedBalanceID, cb.withdrawalAddress)
		}
		
		cb.sendAttemptLog(goroutineID, attempt, hash, amount, err)
		
		if err == nil {
			cb.sendSuccess(fmt.Sprintf("Successfully claimed %.7f PI - Hash: %s", amount, hash))
			cb.cancel() // Stop all other goroutines
			return
		}

		// Aggressive retry with jitter
		sleepDuration := time.Duration(50+attempt*10) * time.Millisecond
		time.Sleep(sleepDuration)
	}
}

// New function for continuous transfer monitoring starting at unlock time
func (cb *ConcurrentBot) continuousTransferMonitor() {
	cb.sendMessage("💰 Starting continuous transfer monitor (every 10ms)")
	
	// Run indefinitely every 10ms until successful transfer
	for {
		select {
		case <-cb.ctx.Done():
			return
		default:
		}

		// Always attempt transfer regardless of balance
		availableBalance, err := cb.wallet.GetAvailableBalance(cb.mainKp)
		if err == nil && availableBalance != "0" && availableBalance != "0.00" {
			// Attempt transfer with high fee
			transferFee := util.GetTransferFee()
			hash, err := cb.wallet.TransferWithFee(cb.mainKp, availableBalance, cb.withdrawalAddress, transferFee)
			
			if err == nil {
				cb.sendSuccess(fmt.Sprintf("Transfer completed: %s PI - Hash: %s", availableBalance, hash))
				return
			} else {
				cb.sendMessage(fmt.Sprintf("Transfer retry: %s", err.Error()))
			}
		}
		
		// Check every 10ms for immediate response as requested
		time.Sleep(10 * time.Millisecond)
	}
}

func (cb *ConcurrentBot) sendMessage(msg string) {
	response := WithdrawResponse{
		Time:    time.Now().Format("15:04:05"),
		Message: msg,
		Success: true,
		Action:  "info",
	}
	cb.mutex.Lock()
	cb.conn.WriteJSON(response)
	cb.mutex.Unlock()
}

func (cb *ConcurrentBot) sendError(msg string) {
	response := WithdrawResponse{
		Time:    time.Now().Format("15:04:05"),
		Message: msg,
		Success: false,
		Action:  "error",
	}
	cb.mutex.Lock()
	cb.conn.WriteJSON(response)
	cb.mutex.Unlock()
}

func (cb *ConcurrentBot) sendSuccess(msg string) {
	response := WithdrawResponse{
		Time:    time.Now().Format("15:04:05"),
		Message: msg,
		Success: true,
		Action:  "success",
	}
	cb.mutex.Lock()
	cb.conn.WriteJSON(response)
	cb.mutex.Unlock()
}

func (cb *ConcurrentBot) sendAttemptLog(goroutineID, attempt int, hash string, amount float64, err error) {
	success := err == nil
	message := hash
	if err != nil {
		message = err.Error()
	}

	response := WithdrawResponse{
		Time:          time.Now().Format("15:04:05"),
		AttemptNumber: attempt,
		Message:       fmt.Sprintf("G%d: %s", goroutineID, message),
		Success:       success,
		Amount:        amount,
		Action:        "attempt",
	}
	
	cb.mutex.Lock()
	cb.conn.WriteJSON(response)
	cb.mutex.Unlock()
}