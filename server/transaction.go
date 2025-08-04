package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"pi/util"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/protocols/horizon"
)

type WithdrawRequest struct {
	SeedPhrase        string `json:"seed_phrase"`
	SponsorPhrase     string `json:"sponsor_phrase,omitempty"`
	LockedBalanceID   string `json:"locked_balance_id"`
	WithdrawalAddress string `json:"withdrawal_address"`
	Amount            string `json:"amount"`
}

type WithdrawResponse struct {
	Time             string  `json:"time"`
	AttemptNumber    int     `json:"attempt_number"`
	RecipientAddress string  `json:"recipient_address"`
	SenderAddress    string  `json:"sender_address"`
	Amount           float64 `json:"amount"`
	Success          bool    `json:"success"`
	Message          string  `json:"message"`
	Action           string  `json:"action"`
	ServerTime       string  `json:"server_time"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var writeMu sync.Mutex

func (s *Server) Withdraw(ctx *gin.Context) {
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		ctx.JSON(500, gin.H{"message": "Failed to upgrade to WebSocket"})
		return
	}
	defer conn.Close()

	var req WithdrawRequest
	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.WriteJSON(gin.H{"message": "Invalid request"})
		return
	}

	err = json.Unmarshal(message, &req)
	if err != nil {
		conn.WriteJSON(gin.H{"message": "Malformed JSON"})
		return
	}

	// Get main keypair
	mainKp, err := util.GetKeyFromSeed(req.SeedPhrase)
	if err != nil {
		s.sendErrorResponse(conn, "Invalid main seed phrase")
		return
	}

	// Get sponsor keypair if provided
	var sponsorKp *keypair.Full
	if req.SponsorPhrase != "" {
		sponsorKp, err = util.GetKeyFromSeed(req.SponsorPhrase)
		if err != nil {
			s.sendErrorResponse(conn, "Invalid sponsor seed phrase")
			return
		}
	}

	// Send server time
	s.sendResponse(conn, WithdrawResponse{
		Action:     "server_time",
		ServerTime: time.Now().Format("15:04:05"),
		Message:    "Bot initialized",
		Success:    true,
	})

	// Start concurrent available balance transfer
	go s.handleAvailableBalance(conn, mainKp, req)

	// Handle locked balance
	s.handleLockedBalance(conn, mainKp, sponsorKp, req)
}

func (s *Server) handleAvailableBalance(conn *websocket.Conn, kp *keypair.Full, req WithdrawRequest) {
	availableBalance, err := s.wallet.GetAvailableBalance(kp)
	if err != nil {
		s.sendResponse(conn, WithdrawResponse{
			Action:  "available_balance_error",
			Message: "Error getting available balance: " + err.Error(),
			Success: false,
		})
		return
	}

	if availableBalance != "0" {
		// Transfer available balance immediately with high fee
		hash, err := s.wallet.TransferWithFee(kp, availableBalance, req.WithdrawalAddress, util.GetTransferFee())
		if err == nil {
			s.sendResponse(conn, WithdrawResponse{
				Action:  "available_transferred",
				Message: fmt.Sprintf("Available balance transferred - Hash: %s", hash),
				Success: true,
			})
		} else {
			s.sendResponse(conn, WithdrawResponse{
				Action:  "available_transfer_failed",
				Message: "Available balance transfer failed: " + err.Error(),
				Success: false,
			})
		}
	}
}

func (s *Server) handleLockedBalance(conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, req WithdrawRequest) {
	balance, err := s.wallet.GetClaimableBalance(req.LockedBalanceID)
	if err != nil {
		s.sendErrorResponse(conn, "Error getting locked balance: "+err.Error())
		return
	}

	// Check if balance is immediately claimable
	claimableAt, ok := util.ExtractClaimableTime(balance.Claimants[0].Predicate)
	if !ok {
		s.sendErrorResponse(conn, "Cannot determine unlock time")
		return
	}

	if time.Now().After(claimableAt) {
		// Already unlocked, start aggressive claiming immediately
		s.startAggressiveBot(conn, mainKp, sponsorKp, req, balance)
	} else {
		// Schedule for future unlock
		s.scheduleAggressiveBot(conn, mainKp, sponsorKp, req, balance, claimableAt)
	}
}

func (s *Server) scheduleAggressiveBot(conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, req WithdrawRequest, balance *horizon.ClaimableBalance, claimableAt time.Time) {
	s.sendResponse(conn, WithdrawResponse{
		Action:  "scheduled",
		Message: fmt.Sprintf("Aggressive bot scheduled for %s (5 seconds early)", claimableAt.Format("15:04:05")),
		Success: true,
	})

	// Start 5 seconds before unlock time
	startTime := claimableAt.Add(-5 * time.Second)
	waitDuration := time.Until(startTime)

	if waitDuration > 0 {
		time.Sleep(waitDuration)
	}

	s.startAggressiveBot(conn, mainKp, sponsorKp, req, balance)
}

func (s *Server) startAggressiveBot(conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, req WithdrawRequest, balance *horizon.ClaimableBalance) {
	bot := NewConcurrentBot(s.wallet, conn, mainKp, sponsorKp, req.WithdrawalAddress, req.Amount, req.LockedBalanceID)
	bot.StartAggressiveBot(balance)
}

func (s *Server) sendResponse(conn *websocket.Conn, res WithdrawResponse) {
	writeMu.Lock()
	defer writeMu.Unlock()
	res.Time = time.Now().Format("15:04:05")
	res.ServerTime = time.Now().Format("15:04:05")
	conn.WriteJSON(res)
}

func (s *Server) sendErrorResponse(conn *websocket.Conn, msg string) {
	writeMu.Lock()
	defer writeMu.Unlock()
	res := WithdrawResponse{
		Time:       time.Now().Format("15:04:05"),
		ServerTime: time.Now().Format("15:04:05"),
		Success:    false,
		Message:    msg,
		Action:     "error",
	}
	conn.WriteJSON(res)
}