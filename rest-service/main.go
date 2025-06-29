package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type User struct {
	ID         int32    `json:"id"`
	Name       string   `json:"name"`
	Email      string   `json:"email"`
	Age        int32    `json:"age"`
	Address    string   `json:"address"`
	Phone      string   `json:"phone"`
	Company    string   `json:"company"`
	Department string   `json:"department"`
	Position   string   `json:"position"`
	Skills     []string `json:"skills"`
}

type GetUserResponse struct {
	User    *User  `json:"user,omitempty"`
	Message string `json:"message"`
}

type GetUsersRequest struct {
	Limit  int32 `json:"limit" form:"limit"`
	Offset int32 `json:"offset" form:"offset"`
}

type GetUsersResponse struct {
	Users   []*User `json:"users"`
	Total   int32   `json:"total"`
	Message string  `json:"message"`
}

type CreateUserRequest struct {
	Name       string   `json:"name" binding:"required"`
	Email      string   `json:"email" binding:"required"`
	Age        int32    `json:"age" binding:"required"`
	Address    string   `json:"address"`
	Phone      string   `json:"phone"`
	Company    string   `json:"company"`
	Department string   `json:"department"`
	Position   string   `json:"position"`
	Skills     []string `json:"skills"`
}

type CreateUserResponse struct {
	User    *User  `json:"user,omitempty"`
	Message string `json:"message"`
}

type UserService struct {
	users map[int32]*User
	mu    sync.RWMutex
}

func NewUserService() *UserService {
	service := &UserService{users: make(map[int32]*User)}

	sampleUsers := []*User{
		{ID: 1, Name: "Alice Johnson", Email: "alice@example.com", Age: 28, Address: "123 Main St, City A", Phone: "+1-555-0101", Company: "TechCorp", Department: "Engineering", Position: "Senior Developer", Skills: []string{"Go", "Python", "Kubernetes", "Docker", "gRPC"}},
		{ID: 2, Name: "Bob Smith", Email: "bob@example.com", Age: 32, Address: "456 Oak Ave, City B", Phone: "+1-555-0102", Company: "DataInc", Department: "Data Science", Position: "Lead Data Scientist", Skills: []string{"Python", "R", "Machine Learning", "SQL", "TensorFlow"}},
		{ID: 3, Name: "Carol Williams", Email: "carol@example.com", Age: 26, Address: "789 Pine Rd, City C", Phone: "+1-555-0103", Company: "WebSolutions", Department: "Frontend", Position: "UI/UX Designer", Skills: []string{"JavaScript", "React", "CSS", "Figma", "Adobe XD"}},
		{ID: 4, Name: "David Brown", Email: "david@example.com", Age: 35, Address: "321 Elm St, City D", Phone: "+1-555-0104", Company: "CloudTech", Department: "DevOps", Position: "DevOps Engineer", Skills: []string{"AWS", "Kubernetes", "Terraform", "Docker", "Jenkins"}},
		{ID: 5, Name: "Eva Davis", Email: "eva@example.com", Age: 29, Address: "654 Maple Dr, City E", Phone: "+1-555-0105", Company: "MobileApps", Department: "Mobile", Position: "Mobile Developer", Skills: []string{"Flutter", "Dart", "Swift", "Kotlin", "React Native"}},
	}

	for _, user := range sampleUsers {
		service.users[user.ID] = user
	}

	return service
}

func simulateLatency() {
	time.Sleep(10 * time.Millisecond)
}

func validateCreateUserRequest(req *CreateUserRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Email == "" {
		return fmt.Errorf("email is required")
	}
	if !strings.Contains(req.Email, "@") {
		return fmt.Errorf("invalid email format")
	}
	if req.Age <= 0 || req.Age > 150 {
		return fmt.Errorf("age must be between 1 and 150")
	}
	return nil
}

func (s *UserService) getUserHandler(c *gin.Context) {
	simulateLatency()

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, GetUserResponse{
			User:    nil,
			Message: "Invalid user ID format",
		})
		return
	}

	s.mu.RLock()
	user, exists := s.users[int32(id)]
	s.mu.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, GetUserResponse{
			User:    nil,
			Message: fmt.Sprintf("User with ID %d not found", id),
		})
		return
	}

	c.JSON(http.StatusOK, GetUserResponse{
		User:    user,
		Message: "User retrieved successfully",
	})
}

func (s *UserService) getUsersHandler(c *gin.Context) {
	simulateLatency()

	var req GetUsersRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, GetUsersResponse{
			Users:   []*User{},
			Total:   0,
			Message: "Invalid query parameters: " + err.Error(),
		})
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	s.mu.RLock()
	allUsers := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		allUsers = append(allUsers, user)
	}
	s.mu.RUnlock()

	start := int(offset)
	end := start + int(limit)

	if start >= len(allUsers) {
		c.JSON(http.StatusOK, GetUsersResponse{
			Users:   []*User{},
			Total:   int32(len(allUsers)),
			Message: "No users found in this range",
		})
		return
	}

	if end > len(allUsers) {
		end = len(allUsers)
	}

	paginatedUsers := allUsers[start:end]

	c.JSON(http.StatusOK, GetUsersResponse{
		Users:   paginatedUsers,
		Total:   int32(len(allUsers)),
		Message: fmt.Sprintf("Retrieved %d users (offset: %d, limit: %d)", len(paginatedUsers), offset, limit),
	})
}

func (s *UserService) createUserHandler(c *gin.Context) {
	simulateLatency()

	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, CreateUserResponse{
			User:    nil,
			Message: "Invalid request body: " + err.Error(),
		})
		return
	}

	if err := validateCreateUserRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, CreateUserResponse{
			User:    nil,
			Message: "Validation failed: " + err.Error(),
		})
		return
	}

	s.mu.Lock()

	newID := int32(len(s.users) + 1)
	for s.users[newID] != nil {
		newID++
	}

	newUser := &User{
		ID:         newID,
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
	s.mu.Unlock()

	c.JSON(http.StatusCreated, CreateUserResponse{
		User:    newUser,
		Message: fmt.Sprintf("User created successfully with ID %d", newID),
	})
}

func setupRoutes(userService *UserService) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "service": "rest-user-service"})
	})

	router.GET("/users/:id", userService.getUserHandler)
	router.GET("/users", userService.getUsersHandler)
	router.POST("/users", userService.createUserHandler)

	return router
}

func main() {
	port := ":8080"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	userService := NewUserService()
	router := setupRoutes(userService)

	log.Printf("REST server starting on port %s", port)

	if err := router.Run(port); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}
