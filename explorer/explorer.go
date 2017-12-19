// Package explorer handles the block explorer subsystem for generating the
// explorer pages.
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.
package explorer

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dcrdata/dcrdata/blockdata"
	"github.com/dcrdata/dcrdata/db/dbtypes"
	"github.com/dcrdata/dcrdata/mempool"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/wire"
	humanize "github.com/dustin/go-humanize"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
)

const (
	homeTemplateIndex int = iota
	rootTemplateIndex
	blockTemplateIndex
	txTemplateIndex
	addressTemplateIndex
	decodeTxTemplateIndex
	errorTemplateIndex
)

const (
	maxExplorerRows          = 2000
	minExplorerRows          = 20
	defaultAddressRows int64 = 20
	maxAddressRows     int64 = 1000
)

// explorerDataSourceLite implements an interface for collecting data for the
// explorer pages
type explorerDataSourceLite interface {
	GetExplorerBlock(hash string) *BlockInfo
	GetExplorerBlocks(start int, end int) []*BlockBasic
	GetBlockHeight(hash string) (int64, error)
	GetBlockHash(idx int64) (string, error)
	GetExplorerTx(txid string) *TxInfo
	GetExplorerAddress(address string, count, offset int64) *AddressInfo
	DecodeRawTransaction(txhex string) (*dcrjson.TxRawResult, error)
	SendRawTransaction(txhex string) (string, error)
	GetHeight() int
	GetChainParams() *chaincfg.Params
}

// explorerDataSource implements extra data retrieval functions that require a
// faster solution than RPC.
type explorerDataSource interface {
	SpendingTransaction(fundingTx string, vout uint32) (string, uint32, int8, error)
	SpendingTransactions(fundingTxID string) ([]string, []uint32, []uint32, error)
	AddressHistory(address string, N, offset int64) ([]*dbtypes.AddressRow, *AddressBalance, error)
	FillAddressTransactions(addrInfo *AddressInfo) error
}

type explorerUI struct {
	Mux             *chi.Mux
	blockData       explorerDataSourceLite
	explorerSource  explorerDataSource
	liteMode        bool
	templates       []*template.Template
	templateFiles   map[string]string
	templateHelpers template.FuncMap
	wsHub           *WebsocketHub
	NewBlockDataMtx sync.RWMutex
	NewBlockData    *BlockBasic
	ExtraInfo       *HomeInfo
	MempoolData     *MempoolInfo
	ChainParams     *chaincfg.Params
}

func (exp *explorerUI) reloadTemplates() error {
	homeTemplate, err := template.New("home").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["home"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	addressTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	decodeTxTemplate, err := template.New("rawtx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["rawtx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	errorTemplate, err := template.New("error").ParseFiles(
		exp.templateFiles["error"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		return err
	}

	exp.templates[homeTemplateIndex] = homeTemplate
	exp.templates[rootTemplateIndex] = explorerTemplate
	exp.templates[blockTemplateIndex] = blockTemplate
	exp.templates[txTemplateIndex] = txTemplate
	exp.templates[addressTemplateIndex] = addressTemplate
	exp.templates[decodeTxTemplateIndex] = decodeTxTemplate
	exp.templates[errorTemplateIndex] = errorTemplate

	return nil
}

// See reloadsig*.go for an exported method
func (exp *explorerUI) reloadTemplatesSig(sig os.Signal) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sig)

	go func() {
		for {
			sigr := <-sigChan
			log.Infof("Received %s", sig)
			if sigr == sig {
				if err := exp.reloadTemplates(); err != nil {
					log.Error(err)
					continue
				}
				log.Infof("Explorer UI html templates reparsed.")
			}
		}
	}()
}

// StopWebsocketHub stops the websocket hub
func (exp *explorerUI) StopWebsocketHub() {
	log.Info("Stopping websocket hub.")
	exp.wsHub.Stop()
}

// New returns an initialized instance of explorerUI
func New(dataSource explorerDataSourceLite, primaryDataSource explorerDataSource,
	useRealIP bool) *explorerUI {
	exp := new(explorerUI)
	exp.Mux = chi.NewRouter()
	exp.blockData = dataSource
	exp.explorerSource = primaryDataSource
	exp.MempoolData = new(MempoolInfo)
	// explorerDataSource is an interface that could have a value of pointer
	// type, and if either is nil this means lite mode.
	if exp.explorerSource == nil || reflect.ValueOf(exp.explorerSource).IsNil() {
		exp.liteMode = true
	}

	if useRealIP {
		exp.Mux.Use(middleware.RealIP)
	}

	exp.ChainParams = exp.blockData.GetChainParams()

	exp.templateFiles = make(map[string]string)
	exp.templateFiles["home"] = filepath.Join("views", "home.tmpl")
	exp.templateFiles["explorer"] = filepath.Join("views", "explorer.tmpl")
	exp.templateFiles["block"] = filepath.Join("views", "block.tmpl")
	exp.templateFiles["tx"] = filepath.Join("views", "tx.tmpl")
	exp.templateFiles["extras"] = filepath.Join("views", "extras.tmpl")
	exp.templateFiles["address"] = filepath.Join("views", "address.tmpl")
	exp.templateFiles["rawtx"] = filepath.Join("views", "rawtx.tmpl")
	exp.templateFiles["error"] = filepath.Join("views", "error.tmpl")

	toInt64 := func(v interface{}) int64 {
		switch vt := v.(type) {
		case int64:
			return vt
		case int32:
			return int64(vt)
		case uint32:
			return int64(vt)
		case uint64:
			return int64(vt)
		case int:
			return int64(vt)
		case int16:
			return int64(vt)
		case uint16:
			return int64(vt)
		default:
			return math.MinInt64
		}
	}

	exp.templateHelpers = template.FuncMap{
		"add": func(a int64, b int64) int64 {
			val := a + b
			return val
		},
		"subtract": func(a int64, b int64) int64 {
			val := a - b
			return val
		},
		"divide": func(n int64, d int64) int64 {
			return n / d
		},
		"timezone": func() string {
			t, _ := time.Now().Zone()
			return t
		},
		"percentage": func(a int64, b int64) float64 {
			p := (float64(a) / float64(b)) * 100
			return p
		},
		"int64": toInt64,
		"intComma": func(v interface{}) string {
			return humanize.Comma(toInt64(v))
		},
		"int64Comma": func(v int64) string {
			return humanize.Comma(v)
		},
		"ticketWindowProgress": func(i int) float64 {
			p := (float64(i) / float64(exp.ChainParams.StakeDiffWindowSize)) * 100
			return p
		},
		"float64AsDecimalParts": func(v float64, useCommas bool) []string {
			clipped := fmt.Sprintf("%.8f", v)
			oldLength := len(clipped)
			clipped = strings.TrimRight(clipped, "0")
			trailingZeros := strings.Repeat("0", oldLength-len(clipped))
			valueChunks := strings.Split(clipped, ".")
			integer := valueChunks[0]
			var dec string
			if len(valueChunks) == 2 {
				dec = valueChunks[1]
			} else {
				dec = ""
				log.Errorf("float64AsDecimalParts has no decimal value. Input: %v", v)
			}
			if useCommas {
				integerAsInt64, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					log.Errorf("float64AsDecimalParts comma formatting failed. Input: %v Error: %v", v, err.Error())
					integer = "ERROR"
					dec = "VALUE"
					zeros := ""
					return []string{integer, dec, zeros}
				}
				integer = humanize.Comma(integerAsInt64)
			}
			return []string{integer, dec, trailingZeros}
		},
		"amountAsDecimalParts": func(v int64, useCommas bool) []string {
			amt := strconv.FormatInt(v, 10)
			if len(amt) <= 8 {
				dec := strings.TrimRight(amt, "0")
				trailingZeros := strings.Repeat("0", len(amt)-len(dec))
				leadingZeros := strings.Repeat("0", 8-len(amt))
				return []string{"0", leadingZeros + dec, trailingZeros}
			}
			integer := amt[:len(amt)-8]
			if useCommas {
				integerAsInt64, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					log.Errorf("amountAsDecimalParts comma formatting failed. Input: %v Error: %v", v, err.Error())
					integer = "ERROR"
					dec := "VALUE"
					zeros := ""
					return []string{integer, dec, zeros}
				}
				integer = humanize.Comma(integerAsInt64)
			}
			dec := strings.TrimRight(amt[len(amt)-8:], "0")
			zeros := strings.Repeat("0", 8-len(dec))
			return []string{integer, dec, zeros}
		},
	}

	exp.templates = make([]*template.Template, 0, 4)

	homeTemplate, err := template.New("home").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["home"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, homeTemplate)

	explorerTemplate, err := template.New("explorer").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["explorer"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, explorerTemplate)

	blockTemplate, err := template.New("block").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["block"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, blockTemplate)

	txTemplate, err := template.New("tx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["tx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, txTemplate)

	addrTemplate, err := template.New("address").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["address"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, addrTemplate)

	decodeTxTemplate, err := template.New("rawtx").Funcs(exp.templateHelpers).ParseFiles(
		exp.templateFiles["rawtx"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, decodeTxTemplate)

	errorTemplate, err := template.New("error").ParseFiles(
		exp.templateFiles["error"],
		exp.templateFiles["extras"],
	)
	if err != nil {
		log.Errorf("Unable to create new html template: %v", err)
	}
	exp.templates = append(exp.templates, errorTemplate)

	exp.addRoutes()

	wsh := NewWebsocketHub()
	go wsh.run()

	exp.wsHub = wsh

	return exp
}

func (exp *explorerUI) Store(blockData *blockdata.BlockData, _ *wire.MsgBlock) error {
	exp.NewBlockDataMtx.Lock()
	bData := blockData.ToBlockExplorerSummary()
	newBlockData := &BlockBasic{
		Height:         int64(bData.Height),
		Voters:         bData.Voters,
		FreshStake:     bData.FreshStake,
		Size:           int32(bData.Size),
		Transactions:   bData.TxLen,
		BlockTime:      bData.Time,
		FormattedTime:  bData.FormattedTime,
		FormattedBytes: humanize.Bytes(uint64(bData.Size)),
		Revocations:    uint32(bData.Revocations),
	}
	exp.NewBlockData = newBlockData
	exp.ExtraInfo = &HomeInfo{
		CoinSupply:       blockData.ExtraInfo.CoinSupply,
		StakeDiff:        blockData.CurrentStakeDiff.CurrentStakeDifficulty,
		IdxBlockInWindow: blockData.IdxBlockInWindow,
		Difficulty:       blockData.Header.Difficulty,
		NBlockSubsidy: BlockSubsidy{
			Dev:   blockData.ExtraInfo.NextBlockSubsidy.Developer,
			PoS:   blockData.ExtraInfo.NextBlockSubsidy.PoS,
			PoW:   blockData.ExtraInfo.NextBlockSubsidy.PoW,
			Total: blockData.ExtraInfo.NextBlockSubsidy.Total,
		},
		Params: ChainParams{
			WindowSize: exp.ChainParams.StakeDiffWindowSize,
		},
	}
	exp.NewBlockDataMtx.Unlock()

	exp.wsHub.HubRelay <- sigNewBlock

	log.Debugf("Got new block %d", newBlockData.Height)

	return nil
}

func (exp *explorerUI) StoreMPData(data *mempool.MempoolData, timestamp time.Time) error {
	exp.MempoolData.RLock()
	exp.MempoolData.NumTickets = data.NumTickets
	exp.MempoolData.RUnlock()
	exp.wsHub.HubRelay <- sigMempoolUpdate

	return nil
}

func (exp *explorerUI) addRoutes() {
	exp.Mux.Use(middleware.Logger)
	exp.Mux.Use(middleware.Recoverer)
	corsMW := cors.Default()
	exp.Mux.Use(corsMW.Handler)

	redirect := func(url string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			x := chi.URLParam(r, "x")
			if x != "" {
				x = "/" + x
			}
			http.Redirect(w, r, "/"+url+x, http.StatusPermanentRedirect)
		}
	}
	exp.Mux.Get("/", redirect("blocks"))

	exp.Mux.Get("/block/{x}", redirect("block"))

	exp.Mux.Get("/tx/{x}", redirect("tx"))

	exp.Mux.Get("/address/{x}", redirect("address"))

	exp.Mux.Get("/decodetx", redirect("decodetx"))
}
