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
	SponsorPhrase     string `json:"sponsor_phrase"`
	WithdrawalAddress string `json:"withdrawal_address"`
	LockedBalanceID   string `json:"locked_balance_id"`
	Amount            string `json:"amount"`
}

type WithdrawResponse struct {
	Action        string  `json:"action"`
	Message       string  `json:"message"`
	Success       bool    `json:"success"`
	Time          string  `json:"time"`
	ServerTime    string  `json:"server_time"`
	AttemptNumber int     `json:"attempt_number,omitempty"`
	Amount        float64 `json:"amount,omitempty"`
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

	// Handle locked balance
	s.handleLockedBalance(conn, mainKp, sponsorKp, req)
}

func (s *Server) handleLockedBalance(conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, req WithdrawRequest) {
	balance, err := s.wallet.GetClaimableBalance(req.LockedBalanceID)
	if err != nil {
		s.sendErrorResponse(conn, "Error getting locked balance: "+err.Error())
		return
	}

	// Check if balance has claimants
	if len(balance.Claimants) == 0 {
		s.sendErrorResponse(conn, "No claimants found for this balance")
		return
	}

	// Find claimant that matches our address
	var claimableAt time.Time
	var found bool
	
	for _, claimant := range balance.Claimants {
		if claimant.Destination == mainKp.Address() {
			claimableAt, found = util.ExtractClaimableTime(claimant.Predicate)
			if found {
				break
			}
		}
	}

	if !found {
		s.sendErrorResponse(conn, "Cannot determine unlock time from balance predicate")
		return
	}

	s.sendResponse(conn, WithdrawResponse{
		Action:  "unlock_time_found",
		Message: fmt.Sprintf("Unlock time found: %s", claimableAt.Format("2006-01-02 15:04:05")),
		Success: true,
	})

	if time.Now().After(claimableAt) {
		// Already unlocked, start aggressive claiming immediately
		s.startAggressiveBot(conn, mainKp, sponsorKp, req, balance, claimableAt)
	} else {
		// Schedule for exact unlock time
		s.scheduleAggressiveBot(conn, mainKp, sponsorKp, req, balance, claimableAt)
	}
}

func (s *Server) scheduleAggressiveBot(conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, req WithdrawRequest, balance *horizon.ClaimableBalance, claimableAt time.Time) {
	s.sendResponse(conn, WithdrawResponse{
		Action:  "scheduled",
		Message: fmt.Sprintf("Aggressive bot scheduled for exact unlock time: %s", claimableAt.Format("15:04:05")),
		Success: true,
	})

	// Wait until exact unlock time
	waitDuration := time.Until(claimableAt)

	if waitDuration > 0 {
		s.sendResponse(conn, WithdrawResponse{
			Action:  "waiting",
			Message: fmt.Sprintf("Waiting %.0f seconds until exact unlock time...", waitDuration.Seconds()),
			Success: true,
		})
		time.Sleep(waitDuration)
	}

	s.startAggressiveBot(conn, mainKp, sponsorKp, req, balance, claimableAt)
}

func (s *Server) startAggressiveBot(conn *websocket.Conn, mainKp, sponsorKp *keypair.Full, req WithdrawRequest, balance *horizon.ClaimableBalance, claimableAt time.Time) {
	bot := NewConcurrentBot(s.wallet, conn, mainKp, sponsorKp, req.WithdrawalAddress, req.Amount, req.LockedBalanceID)
	bot.StartAggressiveBot(balance, claimableAt)
}

func (s *Server) sendResponse(conn *websocket.Conn, res WithdrawResponse) {
	writeMu.Lock()
	defer writeMu.Unlock()
	res.Time = time.Now().Format("15:04:05")
	res.ServerTime = time.Now().Format("15:04:05")
	conn.WriteJSON(res)
}

func (s *Server) sendErrorResponse(conn *websocket.Conn, message string) {
	s.sendResponse(conn, WithdrawResponse{
		Action:  "error",
		Message: message,
		Success: false,
	})
}