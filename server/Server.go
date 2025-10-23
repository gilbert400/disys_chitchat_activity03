package main

import (
	"bufio"
	pb "disys_chitchat_activity03/grpc"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"google.golang.org/grpc"
)

// Our clock
var clock int32

// A Mutex we use when incrementing the clock, to ensure safe threading.
var mu sync.Mutex

// our chitChatServer struct implementing our protobuffer server.
// Also contains a map that maps the username to the active chat stream of each user.
type chitChatServer struct {
	pb.UnimplementedChitChatServer
	clients map[string]pb.ChitChat_SendMessageServer
}

// Broadcast function, that broadcasts a message to every user
func (s *chitChatServer) broadcast(msg *pb.MessageLog) {
	//We lock so we always broadcast messages in the same order for every user.
	//So no other messages can be broadcast and create problems.
	mu.Lock()
	defer mu.Unlock()

	//We loop through the name and the stream of our saved clients.
	for name, clientStream := range s.clients {
		//We do not broadcast the message if it is the message of the person who sent it,
		//As it is displayed on the clients console to start with.
		if name == msg.ClientName {
			continue
		}

		//We create a goroutine to send the actual message to each stream.
		//The goroutine is to ensure we don't have to wait for each client to receive the message,
		//before we can send a message to the next person.
		go func(name string, stream pb.ChitChat_SendMessageServer) {
			if err := stream.Send(msg); err != nil {
				log.Printf("Error sending to %s: %v", name, err)
			}
		}(name, clientStream)
	}
}

// Our actual protocolbuffer implementation, that takes in a stream.
func (s *chitChatServer) SendMessage(stream pb.ChitChat_SendMessageServer) error {
	//Holds clientName of the stream
	var clientName string

	//A While true loop that loops forever as long as the stream is active.
	for {
		//Retrieves message from stream (if any)
		msg, err := stream.Recv()

		//If we return an error, we can assume the client has disconnected
		if err != nil {
			if clientName != "" {
				mu.Lock()
				//Here we increment our clock for leaving.
				clock++
				//We remove the person from our map of clients.
				delete(s.clients, clientName)
				mu.Unlock()

				//We create a new leave message, and broadcast the message to our users, aswell as the servers console.
				leaveText := fmt.Sprintf("participant %s left Chit Chat at logical time %d", clientName, clock)
				leaveMsg := &pb.MessageLog{
					Timestamp:     clock,
					ComponentName: "Server",
					EventType:     leaveText,
					ClientName:    "Server",
				}
				s.broadcast(leaveMsg)
				log.Printf("[Server @ %d] %s", clock, leaveText)
			}
			return nil
		}

		//We increment our clock for the sending of a new message.
		incrementClock(msg)

		// If HasJoined is false, it is the receival of our dummy message,
		//Which infers it is a new client that has joined
		if msg.HasJoined == false {
			//We set the clientName of our string. Used to remove the client on disconnect.
			clientName = msg.ClientName
			mu.Lock()
			//We add the client and stream to our map.
			s.clients[clientName] = stream
			mu.Unlock()

			//We create and broadcast a join message to all clients aswell as our servers console.
			joinText := fmt.Sprintf("Participant %s joined Chit Chat at logical time %d", clientName, clock)

			joinMsg := &pb.MessageLog{
				Timestamp:     clock,
				ComponentName: "Server",
				EventType:     joinText,
				ClientName:    "Server",
				HasJoined:     true,
			}

			log.Printf("[Server @ %d] %s", clock, joinText)

			s.broadcast(joinMsg)
		} else {

			//If hasJoined is true, then it is the receival of a message from a user, and not a newly joined user.
			log.Printf("[%s @ %d] %s: %s", msg.ComponentName, msg.Timestamp, msg.ClientName, msg.EventType)
			s.broadcast(msg)
		}

	}
}

//Helper function to increment our clock on the server and the message to be sent.
func incrementClock(msg *pb.MessageLog) {
	mu.Lock()
	clock = max(clock, msg.Timestamp) + 1
	msg.Timestamp = clock
	mu.Unlock()
}

//Helper function to find max of 2 integers.
func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func main() {
	//We create our listener to open new connection.
	//We choose TCP has it is more reliable, and don't expect to use big amounts of bandwidth
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	//We create our new grpc server and register it to our struct.
	//We also create our clients map object.
	grpcServer := grpc.NewServer()
	pb.RegisterChitChatServer(grpcServer, &chitChatServer{
		clients: make(map[string]pb.ChitChat_SendMessageServer),
	})

	//We create a goroutine to listen to the "server admin" wanting to close the server.
	reader := bufio.NewReader(os.Stdin)
	go func() {
		for {
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			if text == "exit" {
				mu.Lock()
				clock++
				mu.Unlock()
				log.Printf("[Server @ %d] Closing server...", clock)
				grpcServer.Stop()
				break
			}
		}
	}()

	//We increment our clock for starting the server.
	mu.Lock()
	clock++
	mu.Unlock()

	//We server our listener to our grpc server and actually start the server.
	log.Println("ChitChat server started on port :50051\nWrite 'Exit' to stop server...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
