package main

import (
	"context"
	pb "disys_chitchat_activity03/grpc"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type STATE int

const (
	RELEASED STATE = iota
	WANTED
	HELD
)

var (
	id      int
	state   = RELEASED
	clock   int32
	myReqTS int32

	mu   sync.Mutex
	cond = sync.NewCond(&mu)

	peers    []string
	basePort = 8000
)

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

type RicartAgrawalaServer struct {
	pb.UnimplementedRicartAgrawalaServer
}

func (s *RicartAgrawalaServer) Receive(ctx context.Context, req *pb.Request) (*pb.Reply, error) {
	mu.Lock()
	clock = max(clock, req.Timestamp) + 1

	for state == HELD || (state == WANTED && myReqTS < req.Timestamp) {
		done := make(chan struct{})
		go func() { <-ctx.Done(); close(done) }()
		cond.Wait()

		select {
		case <-done:
			mu.Unlock()
			return nil, ctx.Err()
		default:
		}
	}

	clock++
	mu.Unlock()
	fmt.Printf("I AM REPLYING TO PORT %d", basePort+int(req.SenderId))
	return &pb.Reply{}, nil
}

func Enter() {
	mu.Lock()
	clock++
	myReqTS = clock
	fmt.Println("STATE IS WANTED")
	state = WANTED
	mu.Unlock()

	var wg sync.WaitGroup
	fmt.Println("I AM ASKING ALL PEERS")
	for _, addr := range peers {
		addr := addr
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			conn, _ := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))

			defer conn.Close()

			c := pb.NewRicartAgrawalaClient(conn)

			mu.Lock()
			ts := clock // send current Lamport time
			mu.Unlock()

			_, _ = c.Receive(ctx, &pb.Request{
				SenderId:  int32(id),
				Timestamp: ts,
			})

		}()
	}

	wg.Wait()

	mu.Lock()
	fmt.Println("STATE IS HELD - IN THE CRITICAL SECTION")
	state = HELD
	mu.Unlock()
}

func Exit() {
	mu.Lock()
	clock++
	fmt.Println("STATE IS RELEASED")
	state = RELEASED
	mu.Unlock()
	cond.Broadcast()
}

func loadPeers(selfID, num int) (selfAddr string, peers []string) {
	selfAddr = fmt.Sprintf("localhost:%d", basePort+selfID)
	for i := 0; i < num; i++ {
		if i == selfID {
			continue
		}

		peers = append(peers, fmt.Sprintf("localhost:%d", basePort+i))
	}
	return
}

func main() {
	var (
		flagID = flag.Int("id", 0, "this node id (0..n-1)")
		flagN  = flag.Int("n", 1, "number of nodes")
	)
	flag.Parse()

	if *flagN <= 0 || *flagID < 0 || *flagID >= *flagN {
		log.Fatalf("bad flags: id=%d n=%d", *flagID, *flagN)
	}

	id = *flagID
	selfAddr, p := loadPeers(id, *flagN)
	peers = p

	grpcServer := grpc.NewServer()
	pb.RegisterRicartAgrawalaServer(grpcServer, &RicartAgrawalaServer{})

	lis, err := net.Listen("tcp", selfAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", selfAddr, err)
	}

	log.Printf("[node %d] listening on %s; peers=%v", id, selfAddr, peers)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	rand.Seed(time.Now().UnixNano())
	for {
		time.Sleep(time.Duration(400+rand.Intn(600)) * time.Millisecond)

		Enter()

		time.Sleep(time.Duration(300+rand.Intn(500)) * time.Millisecond)

		Exit()
	}
}
