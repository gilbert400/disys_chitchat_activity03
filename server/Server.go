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

// Below we make our predefined identifier
type STATE int

const (
	RELEASED STATE = iota
	WANTED
	HELD
)

// Our global variables. Explained when used...
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

// Max function for lamport clock.
func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// Our server struct.
type RicartAgrawalaServer struct {
	pb.UnimplementedRicartAgrawalaServer
}

// Receive function as pr. Ricart Agrawala
func (s *RicartAgrawalaServer) Receive(ctx context.Context, req *pb.Request) (*pb.Reply, error) {
	//We choose to lock, as our receive checks a lot of our global variables.
	mu.Lock()
	//We update our lamport clock.
	clock = max(clock, req.Timestamp) + 1

	/*We now check whether or not the request from a node should be replied to,
	or if we want or already are in the CS, and therefor shoudl be queued.
	* myReqTS is the clock at the time this node called it's Enter method, as the clock may change while this
	node is waiting to enter the CS.
	*/
	for state == HELD || (state == WANTED && myReqTS < req.Timestamp) {
		/*We make a done channel aswell as a goroutine that checks the context whether we have lost
		connection to the node. It just waits for the context channel to return, and if so we close our done channel.
		We do this in a seperate go routine since waiting for the context channel blocks the thread.
		*/
		done := make(chan struct{})
		go func() { <-ctx.Done(); close(done) }()

		//Here we use a Condition variable and wait for a broadcast from our Exit. This releases the lock
		// and afterwards reacquires it when we receive a broadcast. This esentially also works as our queue,
		// since the Condition Variables releases the lock in order of which the Wait was called.
		cond.Wait()

		//Here we just check if we have received a reply from our done channel. If so, the node is dead,
		//and we therefor release the lock and dont return anything.
		select {
		case <-done:
			mu.Unlock()
			return nil, ctx.Err()
		default:
		}
	}

	//If we reach here, it means that the node asking was prioritised over this current node,
	//or the Exit function has been called. We therefor reply to the node.
	clock++
	mu.Unlock()
	fmt.Printf("I AM REPLYING TO PORT %d\n", basePort+int(req.SenderId))
	return &pb.Reply{}, nil
}

func Enter() {
	//Here we lock since we access global variables.
	//We increase our clock and save the state of our clock to a seperate variable,
	//To ensure correct order on receives (since the clock can increase concurrently).
	mu.Lock()
	clock++
	myReqTS = clock
	fmt.Println("STATE IS WANTED")
	state = WANTED
	mu.Unlock()

	var wg sync.WaitGroup

	fmt.Println("I AM ASKING ALL PEERS")
	//We loop through all our peer nodes.
	for _, addr := range peers {
		//We set the address to a new variable. This is to ensure each goroutine uses a seperate memory address.
		addr := addr
		//We add 1 to our waitgroup
		wg.Add(1)
		//We ask each peer in a seperate goroutine, so we can have each thread blocked until reply.
		go func() {
			//When reply, substract from waitgroup
			defer wg.Done()

			//Create context. Give it a max of 15 second timeout for response
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			//Cancel when done.
			defer cancel()

			//Dial the connection.
			conn, _ := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))

			//Close the connection when done.
			defer conn.Close()

			//Make new Client.
			c := pb.NewRicartAgrawalaClient(conn)

			//Here we lock, as we are accessing our global clock. We set a new local variable to our clock,
			//So we dont lock while waiting for reply.
			mu.Lock()
			ts := clock
			mu.Unlock()

			//We call our receive function. Here the thread is blocked until we receive a reply.
			_, _ = c.Receive(ctx, &pb.Request{
				SenderId:  int32(id),
				Timestamp: ts,
			})

		}()
	}

	//We wait for our waitgroup to be done. When all goroutines waiting for replies from peers is done,
	//We can continue execution.
	wg.Wait()

	//We lock, change our state to held. When we reach here, all our peers have replied.
	mu.Lock()
	fmt.Println("STATE IS HELD - IN THE CRITICAL SECTION")
	state = HELD
	mu.Unlock()
}

func Exit() {
	//We lock, change our clock, set our state to released.
	mu.Lock()
	clock++
	fmt.Println("STATE IS RELEASED")
	state = RELEASED
	mu.Unlock()
	//We broadcast to all peers waiting in queue, that they can reclaim the lock and continue exection.
	//This will reply to all peers in queue from receive method.
	cond.Broadcast()
}

/*
Load Peers are used for the client to know the ip and ports, of the other clients.
The function needs to know the clients own ID, which is given when it is initialised in the console,
as well as the amount of total clients.
*/
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
	// Flags are used for giving information to the clients upon initialisation in the console.
	// To inisialise you write the following: go run . -id=<id> -n=<peer count>
	var (
		flagID = flag.Int("id", 0, "this node id (0..n-1)")
		flagN  = flag.Int("n", 1, "number of nodes")
	)

	flag.Parse() // Parse command line flag, and assign

	// Checks for bad console inputs
	if *flagN <= 0 || *flagID < 0 || *flagID >= *flagN {
		log.Fatalf("bad flags: id=%d n=%d", *flagID, *flagN)
	}

	// Loads list of other clients and assigns global variables
	id = *flagID
	selfAddr, p := loadPeers(id, *flagN)
	peers = p

	// Create own server
	grpcServer := grpc.NewServer()
	pb.RegisterRicartAgrawalaServer(grpcServer, &RicartAgrawalaServer{})

	// Initialise listener and check error
	lis, err := net.Listen("tcp", selfAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", selfAddr, err)
	}

	log.Printf("[node %d] listening on %s; peers=%v", id, selfAddr, peers)

	// Listen on own port for incomming information
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	// Endless loop that simulates random critical section requests
	rand.Seed(time.Now().UnixNano())
	for {
		time.Sleep(time.Duration(1000+rand.Intn(1500)) * time.Millisecond) // Sleep for random time

		Enter() // Try to enter critical section

		time.Sleep(time.Duration(1000+rand.Intn(1500)) * time.Millisecond) // Critical section for some time

		Exit() // Leave critical section after some time
	}
}
