package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	pb "grpc-rest-benchmark/grpc-service/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserService struct {
	pb.UnimplementedUserServiceServer
	mu    sync.RWMutex
	users map[int32]*pb.User
}

func NewUserService() *UserService {
	service := &UserService{users: make(map[int32]*pb.User)}
	sampleUsers := []*pb.User{
		{Id: 1, Name: "Alice Johnson", Email: "alice@example.com", Age: 28, Address: "123 Main St, City A", Phone: "+1-555-0101", Company: "TechCorp", Department: "Engineering", Position: "Senior Developer", Skills: []string{"Go", "Python", "Kubernetes", "Docker", "gRPC"}},
		{Id: 2, Name: "Bob Smith", Email: "bob@example.com", Age: 32, Address: "456 Oak Ave, City B", Phone: "+1-555-0102", Company: "DataInc", Department: "Data Science", Position: "Lead Data Scientist", Skills: []string{"Python", "R", "Machine Learning", "SQL", "TensorFlow"}},
		{Id: 3, Name: "Carol Williams", Email: "carol@example.com", Age: 26, Address: "789 Pine Rd, City C", Phone: "+1-555-0103", Company: "WebSolutions", Department: "Frontend", Position: "UI/UX Designer", Skills: []string{"JavaScript", "React", "CSS", "Figma", "Adobe XD"}},
		{Id: 4, Name: "David Brown", Email: "david@example.com", Age: 35, Address: "321 Elm St, City D", Phone: "+1-555-0104", Company: "CloudTech", Department: "DevOps", Position: "DevOps Engineer", Skills: []string{"AWS", "Kubernetes", "Terraform", "Docker", "Jenkins"}},
		{Id: 5, Name: "Eva Davis", Email: "eva@example.com", Age: 29, Address: "654 Maple Dr, City E", Phone: "+1-555-0105", Company: "MobileApps", Department: "Mobile", Position: "Mobile Developer", Skills: []string{"Flutter", "Dart", "Swift", "Kotlin", "React Native"}},
	}

	for _, user := range sampleUsers {
		service.users[user.Id] = user
	}

	return service
}

func simulateLatency() {
	time.Sleep(10 * time.Millisecond)
}

func validateCreateUserRequest(req *pb.CreateUserRequest) error {
	if req.Name == "" {
		return status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Email == "" {
		return status.Error(codes.InvalidArgument, "email is required")
	}
	if !strings.Contains(req.Email, "@") {
		return status.Error(codes.InvalidArgument, "invalid email format")
	}
	if req.Age <= 0 || req.Age > 150 {
		return status.Error(codes.InvalidArgument, "age must be between 1 and 150")
	}
	return nil
}

func (s *UserService) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	simulateLatency()

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[req.Id]
	if !exists {
		return &pb.GetUserResponse{
			User:    nil,
			Message: fmt.Sprintf("User with ID %d not found", req.Id),
		}, status.Error(codes.NotFound, "user not found")
	}

	return &pb.GetUserResponse{
		User:    user,
		Message: "User retrieved successfully",
	}, nil
}

func (s *UserService) GetUsers(ctx context.Context, req *pb.GetUsersRequest) (*pb.GetUsersResponse, error) {
	simulateLatency()

	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	allUsers := make([]*pb.User, 0, len(s.users))
	for _, user := range s.users {
		allUsers = append(allUsers, user)
	}
	start := int(offset)
	end := start + int(limit)

	if start >= len(allUsers) {
		return &pb.GetUsersResponse{
			Users:   []*pb.User{},
			Total:   int32(len(allUsers)),
			Message: "No users found in this range",
		}, nil
	}

	if end > len(allUsers) {
		end = len(allUsers)
	}

	paginatedUsers := allUsers[start:end]

	return &pb.GetUsersResponse{
		Users:   paginatedUsers,
		Total:   int32(len(allUsers)),
		Message: fmt.Sprintf("Retrieved %d users (offset: %d, limit: %d)", len(paginatedUsers), offset, limit),
	}, nil
}

func (s *UserService) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	simulateLatency()

	if err := validateCreateUserRequest(req); err != nil {
		return &pb.CreateUserResponse{
			User:    nil,
			Message: "Validation failed: " + err.Error(),
		}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	newID := int32(len(s.users) + 1)
	for s.users[newID] != nil {
		newID++
	}

	newUser := &pb.User{
		Id:         newID,
		Name:       req.Name,
		Email:      req.Email,
		Age:        req.Age,
		Address:    req.Address,
		Phone:      req.Phone,
		Company:    req.Company,
		Department: req.Department,
		Position:   req.Position,
		Skills:     req.Skills,
	}

	s.users[newID] = newUser

	return &pb.CreateUserResponse{
		User:    newUser,
		Message: fmt.Sprintf("User created successfully with ID %d", newID),
	}, nil
}

func main() {

	port := ":50051"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	server := grpc.NewServer()
	userService := NewUserService()
	pb.RegisterUserServiceServer(server, userService)

	log.Printf("gRPC server starting on port %s", port)

	if err := server.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
