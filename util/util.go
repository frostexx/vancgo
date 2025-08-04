package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/stellar/go/exp/crypto/derivation"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"
	"github.com/tyler-smith/go-bip39"
)

func GetKeyFromSeed(mnemonic string) (*keypair.Full, error) {
	seed := bip39.NewSeed(mnemonic, "")
	path := "m/44'/314159'/0'"

	fullKey, err := derivation.DeriveForPath(path, seed)
	if err != nil {
		return nil, fmt.Errorf("error deriving path: %v", err)
	}

	kp, err := keypair.FromRawSeed(fullKey.RawSeed())
	if err != nil {
		return nil, fmt.Errorf("error getting keypair from seed: %v", err)
	}

	return kp, nil
}

func GetIndexFile() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %v", err)
	}

	fmt.Println(wd)
	uiDir := filepath.Join(wd, "ui")
	return filepath.Join(uiDir, "index.html"), nil
}

// DecodeRequestBody is a generic function to decode JSON into the given struct
func DecodeRequestBody(r *http.Request, v interface{}) error {
	decoder := json.NewDecoder(r.Body)
	return decoder.Decode(v)
}

// EncodeResponseBody is a generic function to encode the struct into JSON and write the response
func EncodeResponseBody(w http.ResponseWriter, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func ExtractAllowedAfter(pred xdr.ClaimPredicate) time.Time {
	if pred.Type == xdr.ClaimPredicateTypeClaimPredicateNot && pred.NotPredicate != nil {
		inner := *pred.NotPredicate
		if inner.Type == xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime {
			return time.Unix(int64(*inner.AbsBefore), 0)
		}
	}
	return time.Time{}
}

func ExtractClaimableTime(pred xdr.ClaimPredicate) (time.Time, bool) {
	if pred.Type != xdr.ClaimPredicateTypeClaimPredicateNot {
		return time.Time{}, false
	}
	inner := *pred.NotPredicate

	if inner.Type != xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime {
		return time.Time{}, false
	}

	unixSec := int64(*inner.AbsBefore)
	claimTime := time.Unix(unixSec, 0)

	return claimTime, true
}

// Extract claimable time from claimants array
func ExtractClaimableTimeFromClaimants(claimants []interface{}) (time.Time, bool) {
	for _, claimant := range claimants {
		if claimantMap, ok := claimant.(map[string]interface{}); ok {
			if predicate, exists := claimantMap["predicate"]; exists {
				// Try to parse predicate and extract time
				// This is a simplified version - you might need to adjust based on actual data structure
				return time.Now().Add(time.Hour), true // Placeholder
			}
		}
	}
	return time.Time{}, false
}