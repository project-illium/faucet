// Copyright (c) 2022 The illium developers
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.
package main

import (
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jessevdk/go-flags"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/project-illium/ilxd/params"
	"github.com/project-illium/ilxd/repo"
	"github.com/project-illium/ilxd/rpc/pb"
	"github.com/project-illium/walletlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type faucetServer struct {
	blockchainClient pb.BlockchainServiceClient
	walletClient     pb.WalletServiceClient
	address          string
	httpHost         string
	wsHost           string
	dev              bool

	lockedUtxos map[string]bool
	mtx         sync.RWMutex
}

type Options struct {
	Dev  bool   `long:"dev" description:"Use run a development server on localhost"`
	Host string `short:"o" long:"host" description:"The domain name of the host"`
	Cert string `short:"c" long:"tlscert" description:"Path to the TLS certificate file"`
	Key  string `short:"k" long:"tlskey" description:"Path to the TLS key file"`
}

func main() {
	var opts Options
	parser := flags.NewNamedParser("faucet", flags.Default)
	parser.AddGroup("Options", "Configuration options for the faucet", &opts)
	if _, err := parser.Parse(); err != nil {
		return
	}

	certFile := filepath.Join(repo.DefaultHomeDir, "rpc.cert")

	creds, err := credentials.NewClientTLSFromFile(certFile, "localhost")
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.Dial("127.0.0.1:5001", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatal(err)
	}
	blockchainClient := pb.NewBlockchainServiceClient(conn)
	walletClient := pb.NewWalletServiceClient(conn)

	s := faucetServer{
		blockchainClient: blockchainClient,
		walletClient:     walletClient,
		dev:              opts.Dev,
		lockedUtxos:      make(map[string]bool),
		mtx:              sync.RWMutex{},
	}

	resp, err := walletClient.GetAddress(context.Background(), &pb.GetAddressRequest{})
	if err != nil {
		log.Fatal(err)
	}
	s.address = resp.Address
	if !opts.Dev {
		s.httpHost = "https://" + opts.Host
		s.wsHost = "wss://" + opts.Host
	} else {
		s.httpHost = "http://localhost:8080"
		s.wsHost = "ws://localhost:8080"
	}

	wsHub := newHub()
	go wsHub.run()

	go func() {
		blockStream, err := s.blockchainClient.SubscribeBlocks(context.Background(), &pb.SubscribeBlocksRequest{
			FullBlock:        true,
			FullTransactions: false,
		})
		if err != nil {
			log.Fatal(err)
		}

		for {
			blk, err := blockStream.Recv()
			if err != nil {
				log.Printf("block stream receive error: %s\n", err)
				return
			}

			pid, err := peer.IDFromBytes(blk.BlockInfo.Producer_ID)
			if err != nil {
				log.Printf("block stream receive error: %s\n", err)
				return
			}

			bd := &blkData{
				BlockID:    hex.EncodeToString(blk.BlockInfo.Block_ID),
				Height:     blk.BlockInfo.Height,
				ProducerID: pid.String(),
				Txids:      make([]string, 0, len(blk.GetTransactions())),
			}

			for _, tx := range blk.GetTransactions() {
				bd.Txids = append(bd.Txids, hex.EncodeToString(tx.GetTransaction_ID()))
			}

			out, err := json.MarshalIndent(bd, "", "    ")
			if err != nil {
				log.Printf("block stream receive error: %s\n", err)
				return
			}
			wsHub.Broadcast <- out
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Minute * 30)

		for range ticker.C {
			go s.consolidateUtxos()
		}
	}()

	mx := http.NewServeMux()
	r := mux.NewRouter()
	r.Methods("OPTIONS")
	r.HandleFunc("/blocks/{fromHeight}", s.handleGetBlocks).Methods("GET")
	r.HandleFunc("/getcoins", s.handleGetCoins).Methods("POST")
	r.PathPrefix("/").Handler(http.HandlerFunc(s.serveStaticFile))
	mx.Handle("/", r)
	mx.Handle("/ws", newWebsocketHandler(wsHub))

	if opts.Dev {
		if err := http.ListenAndServe(":8080", mx); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := http.ListenAndServeTLS(":443", opts.Cert, opts.Key, mx); err != nil {
			log.Fatal(err)
		}
	}
}

//go:embed static/*
var embeddedFiles embed.FS

func (s *faucetServer) serveStaticFile(w http.ResponseWriter, r *http.Request) {
	// Strip the "/static" prefix and clean the path
	filePath := path.Clean(r.URL.Path[len("/"):])

	// Use the embedded file system
	fileSystem, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if filePath == "." || filePath == "index.html" {
		s.serveIndexHtml(w, fileSystem)
		return
	}
	if filePath == "index.js" {
		s.serveIndexJs(w, fileSystem)
		return
	}

	// Create a sub file system from the specified path
	f, err := fileSystem.Open(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	f.Close()

	// Serve the file from the embedded file system
	http.FileServer(http.FS(fileSystem)).ServeHTTP(w, r)
}

type blkData struct {
	BlockID    string   `json:"blockID"`
	Height     uint32   `json:"height"`
	ProducerID string   `json:"producerID"`
	Txids      []string `json:"txids"`
}

func (s *faucetServer) handleGetBlocks(w http.ResponseWriter, r *http.Request) {
	from := mux.Vars(r)["fromHeight"]
	idx, err := strconv.Atoi(from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if idx == 0 {
		fmt.Fprint(w, "[]")
		return
	}
	blks := make([]*blkData, 0, 10)
	if idx < 0 {
		resp, err := s.blockchainClient.GetBlockchainInfo(context.Background(), &pb.GetBlockchainInfoRequest{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		idx = int(resp.BestHeight)
	}

	for i := 0; i < 10; i++ {
		resp, err := s.blockchainClient.GetBlock(context.Background(), &pb.GetBlockRequest{
			IdOrHeight: &pb.GetBlockRequest_Height{Height: uint32(idx)},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		bd := &blkData{
			BlockID: resp.Block.ID().String(),
			Height:  resp.Block.Header.Height,
			Txids:   make([]string, 0, len(resp.Block.Transactions)),
		}

		if len(resp.Block.Header.Producer_ID) != 0 {
			pid, err := peer.IDFromBytes(resp.Block.Header.Producer_ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			bd.ProducerID = pid.String()
		}

		for _, tx := range resp.Block.Transactions {
			bd.Txids = append(bd.Txids, tx.ID().String())
		}

		blks = append(blks, bd)
		if resp.Block.Header.Height == 0 {
			break
		}
		idx--
	}

	out, err := json.MarshalIndent(blks, "", "    ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprint(w, string(out))
}

func (s *faucetServer) handleGetCoins(w http.ResponseWriter, r *http.Request) {
	type message struct {
		Addr string `json:"addr"`
	}
	var m message
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.dev {
		if _, err := walletlib.DecodeAddress(m.Addr, &params.RegestParams); err != nil {
			http.Error(w, "invalid payment address", http.StatusBadRequest)
			return
		}
	} else {
		if _, err := walletlib.DecodeAddress(m.Addr, &params.AlphanetParams); err != nil {
			http.Error(w, "invalid payment address", http.StatusBadRequest)
			return
		}
	}

	resp, err := s.walletClient.GetUtxos(context.Background(), &pb.GetUtxosRequest{})
	if err != nil {
		http.Error(w, "error fetching wallet utxos", http.StatusInternalServerError)
		return
	}

	var (
		amt         = uint64(100000000)
		total       = uint64(0)
		commitments [][]byte
	)

	s.mtx.Lock()
	// Only add utxos that
	// - aren't locked
	// - aren't staked
	// - are greater than half the payout amount
	// (this avoids selecting lots of small utxos that will
	// increase the proving time)
	for _, utxo := range resp.Utxos {
		if !s.lockedUtxos[hex.EncodeToString(utxo.Commitment)] && !utxo.Staked && utxo.Amount > amt/2 {
			commitments = append(commitments, utxo.Commitment)
			total += utxo.Amount
			s.lockedUtxos[hex.EncodeToString(utxo.Commitment)] = true
			if total >= amt {
				break
			}
		}
	}
	s.mtx.Unlock()

	if total < amt {
		http.Error(w, "Faucet has no money. Check back later.", http.StatusBadRequest)
		return
	}

	go func(commitmentsToSpend [][]byte, toAddr string) {
		time.AfterFunc(time.Minute*10, func() {
			s.mtx.Lock()
			defer s.mtx.Unlock()
			for _, c := range commitmentsToSpend {
				delete(s.lockedUtxos, hex.EncodeToString(c))
			}
		})

		_, err = s.walletClient.Spend(context.Background(), &pb.SpendRequest{
			ToAddress:        toAddr,
			Amount:           100000000,
			InputCommitments: commitmentsToSpend,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}(commitments, m.Addr)
	fmt.Fprint(w, string("{}"))
}

func (s *faucetServer) consolidateUtxos() {
	resp, err := s.walletClient.GetUtxos(context.Background(), &pb.GetUtxosRequest{})
	if err != nil {
		fmt.Printf("Error fetching wallet utxos: %s\n", err.Error())
		return
	}

	var (
		amt          = uint64(100000000)
		commitments  [][]byte
		hasLargeUtxo = false
	)

	s.mtx.Lock()
	// Add a max of 5 utoxs that aren't:
	// - locked
	// - staked
	// - greater than half the payout amount
	//
	// Plus one larger utxo to make sure that
	// we can cover the fee.
	for _, utxo := range resp.Utxos {
		if !s.lockedUtxos[hex.EncodeToString(utxo.Commitment)] &&
			!utxo.Staked &&
			utxo.Amount > amt && !hasLargeUtxo {

			commitments = append(commitments, utxo.Commitment)
			hasLargeUtxo = true
			continue
		}

		if !s.lockedUtxos[hex.EncodeToString(utxo.Commitment)] &&
			!utxo.Staked &&
			utxo.Amount <= amt/2 &&
			len(commitments) < 5 {

			commitments = append(commitments, utxo.Commitment)
			s.lockedUtxos[hex.EncodeToString(utxo.Commitment)] = true
		}
		if len(commitments) >= 5 {
			break
		}
	}
	s.mtx.Unlock()

	if !hasLargeUtxo {
		return
	}

	time.AfterFunc(time.Minute*20, func() {
		s.mtx.Lock()
		defer s.mtx.Unlock()
		for _, c := range commitments {
			delete(s.lockedUtxos, hex.EncodeToString(c))
		}
	})

	_, err = s.walletClient.SweepWallet(context.Background(), &pb.SweepWalletRequest{
		ToAddress:        s.address,
		InputCommitments: commitments,
	})
	if err != nil {
		fmt.Printf("Error sweeping utxos: %s\n", err.Error())
		return
	}
}

type HtmlTemplate struct {
	Address string
}

func (s *faucetServer) serveIndexHtml(w http.ResponseWriter, fileSystem fs.FS) {
	t, err := template.ParseFS(fileSystem, "index.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := HtmlTemplate{
		Address: s.address,
	}

	w.Header().Set("Content-Type", "text/html")
	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

type JsTemplate struct {
	HttpUrl string
	WsUrl   string
}

func (s *faucetServer) serveIndexJs(w http.ResponseWriter, fileSystem fs.FS) {
	t, err := template.New("index.js").Delims("[[", "]]").ParseFS(fileSystem, "index.js")
	if err != nil {
		fmt.Println("here", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/javascript")
	data := JsTemplate{
		HttpUrl: s.httpHost,
		WsUrl:   s.wsHost,
	}

	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
