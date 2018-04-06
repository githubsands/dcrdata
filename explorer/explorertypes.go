// Copyright (c) 2018, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"fmt"
	"sync"

	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrdata/db/dbtypes"
	"github.com/decred/dcrdata/txhelpers"
)

// BlockBasic models data for the explorer's explorer page
type BlockBasic struct {
	Height         int64  `json:"height"`
	Size           int32  `json:"size"`
	Valid          bool   `json:"valid"`
	Voters         uint16 `json:"votes"`
	Transactions   int    `json:"tx"`
	FreshStake     uint8  `json:"tickets"`
	Revocations    uint32 `json:"revocations"`
	BlockTime      int64  `json:"time"`
	FormattedTime  string `json:"formatted_time"`
	FormattedBytes string
}

// TxBasic models data for transactions on the block page
type TxBasic struct {
	TxID          string
	FormattedSize string
	Total         float64
	Fee           dcrutil.Amount
	FeeRate       dcrutil.Amount
	VoteInfo      *VoteInfo
	Coinbase      bool
}

//AddressTx models data for transactions on the address page
type AddressTx struct {
	TxID          string
	InOutID       uint32
	FormattedSize string
	Total         float64
	Confirmations uint64
	Time          int64
	FormattedTime string
	ReceivedTotal float64
	SentTotal     float64
}

// IOID formats an identification string for the transaction input (or output)
// represented by the AddressTx.
func (a *AddressTx) IOID() string {
	// When AddressTx is used properly, at least one of ReceivedTotal or
	// SentTotal should be zero.
	if a.ReceivedTotal > a.SentTotal {
		// an outpoint receiving funds
		return fmt.Sprintf("%s:out[%d]", a.TxID, a.InOutID)
	}
	// a transaction input referencing an outpoint being spent
	return fmt.Sprintf("%s:in[%d]", a.TxID, a.InOutID)
}

// TxInfo models data needed for display on the tx page
type TxInfo struct {
	*TxBasic
	SpendingTxns    []TxInID
	Type            string
	Vin             []Vin
	Vout            []Vout
	BlockHeight     int64
	BlockIndex      uint32
	Confirmations   int64
	Time            int64
	FormattedTime   string
	Mature          string
	VoteFundsLocked string
	TicketInfo
}

// TicketInfo is used to represent data shown for a sstx transaction.
type TicketInfo struct {
	TicketMaturity       int64
	TimeTillMaturity     float64 // Time before a particular ticket reaches maturity
	PoolStatus           string
	SpendStatus          string
	TicketPoolSize       int64   // Total number of ticket in the pool
	TicketExpiry         int64   // Total number of blocks before a ticket expires
	TicketExpiryDaysLeft float64 // Approximate days left before the given ticket expires
	ShortConfirms        int64   // Total number of confirms up until the point the ticket votes or expires
	BestLuck             int64   // Best possible Luck for voting
	AvgLuck              int64   // Average Luck for voting
	VoteLuck             float64 // Actual Luck for voting on a ticket
	LuckStatus           string  // Short discription based on the VoteLuck
}

// TxInID models the identity of a spending transaction input
type TxInID struct {
	Hash  string
	Index uint32
}

// VoteInfo models data about a SSGen transaction (vote)
type VoteInfo struct {
	Validation         BlockValidation         `json:"block_validation"`
	Version            uint32                  `json:"vote_version"`
	Bits               uint16                  `json:"vote_bits"`
	Choices            []*txhelpers.VoteChoice `json:"vote_choices"`
	TicketSpent        string                  `json:"ticket_spent"`
	MempoolTicketIndex int                     `json:"mempool_ticket_index"`
}

// BlockValidation models data about a vote's decision on a block
type BlockValidation struct {
	Hash     string `json:"hash"`
	Height   int64  `json:"height"`
	Validity bool   `json:"validity"`
}

// Vin models basic data about a tx input for display
type Vin struct {
	*dcrjson.Vin
	Addresses       []string
	FormattedAmount string
}

// Vout models basic data about a tx output for display
type Vout struct {
	Addresses       []string
	Amount          float64
	FormattedAmount string
	Type            string
	Spent           bool
	OP_RETURN       string
}

// BlockInfo models data for display on the block page
type BlockInfo struct {
	*BlockBasic
	Hash                  string
	Version               int32
	Confirmations         int64
	StakeRoot             string
	MerkleRoot            string
	TxAvailable           bool
	Tx                    []*TxBasic
	Tickets               []*TxBasic
	Revs                  []*TxBasic
	Votes                 []*TxBasic
	Misses                []string
	Nonce                 uint32
	VoteBits              uint16
	FinalState            string
	PoolSize              uint32
	Bits                  string
	SBits                 float64
	Difficulty            float64
	ExtraData             string
	StakeVersion          uint32
	PreviousHash          string
	NextHash              string
	TotalSent             float64
	MiningFee             dcrutil.Amount
	StakeValidationHeight int64
}

// AddressInfo models data for display on the address page
type AddressInfo struct {
	// Address is the decred address on the current page
	Address string

	// Page parameters
	MaxTxLimit    int64
	Fullmode      bool
	Path          string
	Limit, Offset int64  // ?n=Limit&start=Offset
	TxnType       string // ?txntype=TxnType

	// NumUnconfirmed is the number of unconfirmed txns for the address
	NumUnconfirmed int64

	// Transactions on the current page
	Transactions    []*AddressTx
	TxnsFunding     []*AddressTx
	TxnsSpending    []*AddressTx
	NumTransactions int64 // The number of transactions in the address
	NumFundingTxns  int64 // number paying to the address
	NumSpendingTxns int64 // number spending outpoints associated with the address
	AmountReceived  dcrutil.Amount
	AmountSent      dcrutil.Amount
	AmountUnspent   dcrutil.Amount

	// Balance is used in full mode, describing all known transactions
	Balance *AddressBalance

	// KnownTransactions refers to the total transaction count in the DB when in
	// full mode, the sum of funding (crediting) and spending (debiting) txns.
	KnownTransactions int64
	KnownFundingTxns  int64
	KnownSpendingTxns int64
}

// TxnCount returns the number of transaction "rows" available.
func (a *AddressInfo) TxnCount() int64 {
	if !a.Fullmode {
		return a.KnownTransactions
	}
	switch dbtypes.AddrTxnTypeFromStr(a.TxnType) {
	case dbtypes.AddrTxnAll:
		return a.KnownTransactions
	case dbtypes.AddrTxnCredit:
		return a.KnownFundingTxns
	case dbtypes.AddrTxnDebit:
		return a.KnownSpendingTxns
	default:
		log.Warnf("Unknown address transaction type: %v", a.TxnType)
		return 0
	}
}

// AddressBalance represents the number and value of spent and unspent outputs
// for an address.
type AddressBalance struct {
	Address      string
	NumSpent     int64
	NumUnspent   int64
	TotalSpent   int64
	TotalUnspent int64
}

// HomeInfo represents data used for the home page
type HomeInfo struct {
	CoinSupply        int64          `json:"coin_supply"`
	StakeDiff         float64        `json:"sdiff"`
	IdxBlockInWindow  int            `json:"window_idx"`
	IdxInRewardWindow int            `json:"reward_idx"`
	Difficulty        float64        `json:"difficulty"`
	DevFund           int64          `json:"dev_fund"`
	DevAddress        string         `json:"dev_address"`
	TicketROI         float64        `json:"roi"`
	ROIPeriod         string         `json:"roi_period"`
	NBlockSubsidy     BlockSubsidy   `json:"subsidy"`
	Params            ChainParams    `json:"params"`
	PoolInfo          TicketPoolInfo `json:"pool_info"`
}

// BlockSubsidy is an implementation of dcrjson.GetBlockSubsidyResult
type BlockSubsidy struct {
	Total int64 `json:"total"`
	PoW   int64 `json:"pow"`
	PoS   int64 `json:"pos"`
	Dev   int64 `json:"dev"`
}

// MempoolInfo models data to update mempool info on the home page
type MempoolInfo struct {
	sync.RWMutex
	MempoolShort
	Transactions []MempoolTx `json:"tx"`
	Tickets      []MempoolTx `json:"tickets"`
	Votes        []MempoolTx `json:"votes"`
	Revocations  []MempoolTx `json:"revs"`
}

// MempoolShort represents the mempool data sent as the mempool update
type MempoolShort struct {
	LastBlockHeight    int64          `json:"block_height"`
	LastBlockTime      int64          `json:"block_time"`
	TotalOut           float64        `json:"total"`
	TotalSize          int32          `json:"size"`
	NumTickets         int            `json:"num_tickets"`
	NumVotes           int            `json:"num_votes"`
	NumRegular         int            `json:"num_regular"`
	NumRevokes         int            `json:"num_revokes"`
	NumAll             int            `json:"num_all"`
	LatestTransactions []MempoolTx    `json:"latest"`
	FormattedTotalSize string         `json:"formatted_size"`
	TicketIndexes      map[string]int `json:"ticket_indexes"`
}

// ChainParams models simple data about the chain server's parameters used for some
// info on the front page
type ChainParams struct {
	WindowSize       int64 `json:"window_size"`
	RewardWindowSize int64 `json:"reward_window_size"`
	TargetPoolSize   int64 `json:"target_pool_size"`
	BlockTime        int64 `json:"target_block_time"`
}

// ReduceAddressHistory generates a template AddressInfo from a slice of
// dbtypes.AddressRow. All fields except NumUnconfirmed and Transactions are set
// completely. Transactions is partially set, with each transaction having only
// the TxID and ReceivedTotal set. The rest of the data should be filled in by
// other means, such as RPC calls or database queries.
func ReduceAddressHistory(addrHist []*dbtypes.AddressRow) *AddressInfo {
	if len(addrHist) == 0 {
		return nil
	}

	var received, sent int64
	var transactions, creditTxns, debitTxns []*AddressTx
	for _, addrOut := range addrHist {
		coin := dcrutil.Amount(addrOut.Value).ToCoin()

		// Funding transaction
		received += int64(addrOut.Value)
		fundingTx := AddressTx{
			TxID:          addrOut.FundingTxHash,
			InOutID:       addrOut.FundingTxVoutIndex,
			ReceivedTotal: coin,
		}
		transactions = append(transactions, &fundingTx)
		creditTxns = append(creditTxns, &fundingTx)

		// Is the outpoint spent?
		if addrOut.SpendingTxHash == "" {
			continue
		}

		// Spending transaction
		sent += int64(addrOut.Value)
		spendingTx := AddressTx{
			TxID:      addrOut.SpendingTxHash,
			InOutID:   addrOut.SpendingTxVinIndex,
			SentTotal: coin,
		}
		transactions = append(transactions, &spendingTx)
		debitTxns = append(debitTxns, &spendingTx)
	}

	return &AddressInfo{
		Address:         addrHist[0].Address,
		Transactions:    transactions,
		TxnsFunding:     creditTxns,
		TxnsSpending:    debitTxns,
		NumFundingTxns:  int64(len(creditTxns)),
		NumSpendingTxns: int64(len(debitTxns)),
		AmountReceived:  dcrutil.Amount(received),
		AmountSent:      dcrutil.Amount(sent),
		AmountUnspent:   dcrutil.Amount(received - sent),
	}
}

// WebsocketBlock wraps the new block info for use in the websocket
type WebsocketBlock struct {
	Block *BlockBasic `json:"block"`
	Extra *HomeInfo   `json:"extra"`
}

// TicketPoolInfo describes the live ticket pool
type TicketPoolInfo struct {
	Size          uint32  `json:"size"`
	Value         float64 `json:"value"`
	ValAvg        float64 `json:"valavg"`
	Percentage    float64 `json:"percent"`
	Target        uint16  `json:"target"`
	PercentTarget float64 `json:"percent_target"`
}

// MempoolTx models the tx basic data for the mempool page
type MempoolTx struct {
	Hash     string    `json:"hash"`
	Time     int64     `json:"time"`
	Size     int32     `json:"size"`
	TotalOut float64   `json:"total"`
	Type     string    `json:"Type"`
	VoteInfo *VoteInfo `json:"vote_info"`
}

// NewMempoolTx models data sent from the notification handler
type NewMempoolTx struct {
	Time int64
	Hex  string
}
