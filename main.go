// Copyright (c) 2022 The illium developers
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/project-illium/ilxd/rpc/pb"
	"github.com/project-illium/ilxd/types"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

type faucetServer struct {
	client pb.BlockchainServiceClient
}

func main() {
	// Handle the root route with the index.html file
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Serve the app.js file
	http.Handle("/index.js", http.FileServer(http.Dir(".")))
	http.Handle("/styles.css", http.FileServer(http.Dir(".")))

	creds, err := credentials.NewClientTLSFromFile("/home/chris/.ilxd/rpc.cert", "localhost")
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.Dial("127.0.0.1:5001", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatal(err)
	}
	client := pb.NewBlockchainServiceClient(conn)
	s := faucetServer{
		client: client,
	}

	blockStream, err := client.SubscribeBlocks(context.Background(), &pb.SubscribeBlocksRequest{
		FullBlock:        true,
		FullTransactions: false,
	})
	if err != nil {
		log.Fatal(err)
	}

	mx := http.NewServeMux()
	r := mux.NewRouter()
	r.Methods("OPTIONS")
	r.HandleFunc("/blocks/{fromHeight}", s.handleGetBlocks).Methods("GET")
	r.PathPrefix("/").Handler(http.HandlerFunc(serveStaticFile))
	mx.Handle("/", r)

	wts := webtransport.Server{
		H3: http3.Server{Addr: ":443"},
	}
	// Create a new HTTP endpoint /webtransport.
	http.HandleFunc("/webtransport", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wts.Upgrade(w, r)
		if err != nil {
			log.Printf("upgrading failed: %s\n", err)
			w.WriteHeader(500)
			return
		}
		stream, err := conn.OpenStream()
		if err != nil {
			log.Printf("block stream receive error: %s\n", err)
			return
		}
		/*time.AfterFunc(time.Second*5, func() {
			bd := &blkData{
				BlockID: types.NewID([]byte{0x11, 0x22}).String(),
				Height:  99,
			}

			out, err := json.MarshalIndent(bd, "", "    ")
			if err != nil {
				log.Printf("block stream receive error: %s\n", err)
				return
			}
			stream.Write(out)
		})*/
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
				BlockID:    types.NewID(blk.BlockInfo.Block_ID).String(),
				Height:     blk.BlockInfo.Height,
				ProducerID: pid.String(),
				Txids:      make([]string, 0, len(blk.GetTransactions())),
			}

			for _, tx := range blk.GetTransactions() {
				bd.Txids = append(bd.Txids, types.NewID(tx.GetTransaction_ID()).String())
			}

			out, err := json.MarshalIndent(bd, "", "    ")
			if err != nil {
				log.Printf("block stream receive error: %s\n", err)
				return
			}
			stream.Write(out)
		}
	})

	go wts.ListenAndServeTLS(os.Args[1], os.Args[2])

	http.ListenAndServeTLS(":443", os.Args[1], os.Args[2], mx)
}

func serveStaticFile(w http.ResponseWriter, r *http.Request) {
	filePath, err := filepath.Abs("static" + r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.ServeFile(w, r, filePath)
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
		resp, err := s.client.GetBlockchainInfo(context.Background(), &pb.GetBlockchainInfoRequest{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		idx = int(resp.BestHeight)
	}

	for i := 0; i < 10; i++ {
		resp, err := s.client.GetBlock(context.Background(), &pb.GetBlockRequest{
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
