package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "grpc-rest-benchmark/grpc-service/proto"

	"github.com/shirou/gopsutil/v3/cpu"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

type TestScenario string

const (
	SmallPayload   TestScenario = "SmallPayload"
	LargePayload   TestScenario = "LargePayload"
	ConcurrentLoad TestScenario = "ConcurrentLoad"
	BurstLoad      TestScenario = "BurstLoad"
	SustainedLoad  TestScenario = "SustainedLoad"
)

type Protocol string

const (
	GRPC Protocol = "gRPC"
	REST Protocol = "REST"
)

type EndpointType string

const (
	GetUser    EndpointType = "GetUser"
	GetUsers   EndpointType = "GetUsers"
	CreateUser EndpointType = "CreateUser"
)

type BenchmarkResult struct {
	Scenario        TestScenario  `csv:"Scenario"`
	Protocol        Protocol      `csv:"Protocol"`
	Endpoint        EndpointType  `csv:"Endpoint"`
	TotalRequests   int64         `csv:"TotalRequests"`
	SuccessRequests int64         `csv:"SuccessRequests"`
	FailedRequests  int64         `csv:"FailedRequests"`
	ErrorRate       float64       `csv:"ErrorRate"`
	Duration        time.Duration `csv:"DurationMs"`
	AvgLatency      time.Duration `csv:"AvgLatencyMs"`
	P50Latency      time.Duration `csv:"P50LatencyMs"`
	P95Latency      time.Duration `csv:"P95LatencyMs"`
	P99Latency      time.Duration `csv:"P99LatencyMs"`
	RPS             float64       `csv:"RPS"`
	CPUPercent      float64       `csv:"CPUPercent"`
	MemoryMB        float64       `csv:"MemoryMB"`
	BytesIn         int64         `csv:"BytesIn"`
	BytesOut        int64         `csv:"BytesOut"`
	ConcurrentUsers int           `csv:"ConcurrentUsers"`
}

type RequestMetrics struct {
	Latency   time.Duration
	Success   bool
	BytesIn   int64
	BytesOut  int64
	Timestamp time.Time
}

type BenchmarkConfig struct {
	GRPCAddress  string
	RESTAddress  string
	TestDuration time.Duration
	WarmupTime   time.Duration
}

type Benchmarker struct {
	config     BenchmarkConfig
	grpcClient pb.UserServiceClient
	grpcConn   *grpc.ClientConn
	httpClient *http.Client
	results    []BenchmarkResult
	mu         sync.RWMutex
}

func NewBenchmarker(config BenchmarkConfig) (*Benchmarker, error) {
	b := &Benchmarker{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		results:    make([]BenchmarkResult, 0),
	}

	conn, err := grpc.Dial(config.GRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %v", err)
	}
	b.grpcConn = conn
	b.grpcClient = pb.NewUserServiceClient(conn)

	return b, nil
}

func (b *Benchmarker) Close() {
	if b.grpcConn != nil {
		b.grpcConn.Close()
	}
}

func (b *Benchmarker) generateTestUsers(size string) []*TestUser {
	users := make([]*TestUser, 0)

	if size == "small" {
		users = append(users, &TestUser{
			Name:       "John Doe",
			Email:      "john@example.com",
			Age:        30,
			Address:    "123 Main St",
			Phone:      "+1-555-0123",
			Company:    "TechCorp",
			Department: "Engineering",
			Position:   "Developer",
			Skills:     []string{"Go", "Python", "JavaScript"},
		})
	} else {
		largeSkills := make([]string, 100)
		for i := 0; i < 100; i++ {
			largeSkills[i] = fmt.Sprintf("Advanced-Skill-%d-with-detailed-professional-description-and-certification-requirements-level-expert-senior-architect", i)
		}

		users = append(users, &TestUser{
			Name:       "Dr. Alice Johnson PhD MSc BSc - Senior Principal Software Engineering Architect and Technical Lead",
			Email:      "alice.johnson.principal.senior.architect.technical.lead@enterprise-advanced-technology-solutions-corporation.international.com",
			Age:        35,
			Address:    "Building A - Floor 15 - Office 1501-A - 123 Very Long Professional Business Street Name In A Metropolitan City With Multiple Districts And Complex Postal Code System, Advanced Technology Business District, Corporate Technology Center",
			Phone:      "+1-555-0123-4567-8901-extension-2500",
			Company:    "Enterprise Advanced Technology Solutions & Professional Software Development Corporation International Ltd. Inc.",
			Department: "Advanced Software Engineering & Research Development Division - Principal Architect Team",
			Position:   "Senior Principal Software Engineer & Technical Lead Architect - Advanced Systems Integration Specialist",
			Skills:     largeSkills,
		})
	}

	return users
}

type TestUser struct {
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

func (b *Benchmarker) collectSystemMetrics() (float64, float64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	memoryMB := float64(m.Alloc) / 1024 / 1024

	cpuPercents, err := cpu.Percent(1*time.Second, false)
	cpuPercent := 0.0
	if err == nil && len(cpuPercents) > 0 {
		cpuPercent = cpuPercents[0]
	}

	return cpuPercent, memoryMB
}

func calculatePercentile(latencies []time.Duration, percentile float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	index := int(math.Ceil(float64(len(latencies))*percentile/100.0)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(latencies) {
		index = len(latencies) - 1
	}

	return latencies[index]
}

func (b *Benchmarker) processResults(scenario TestScenario, protocol Protocol, endpoint EndpointType,
	metrics []RequestMetrics, concurrentUsers int, startTime time.Time) BenchmarkResult {

	totalRequests := int64(len(metrics))
	successRequests := int64(0)
	failedRequests := int64(0)
	totalBytesIn := int64(0)
	totalBytesOut := int64(0)
	latencies := make([]time.Duration, 0, len(metrics))
	totalLatency := time.Duration(0)

	for _, metric := range metrics {
		if metric.Success {
			successRequests++
		} else {
			failedRequests++
		}
		totalBytesIn += metric.BytesIn
		totalBytesOut += metric.BytesOut
		latencies = append(latencies, metric.Latency)
		totalLatency += metric.Latency
	}

	errorRate := float64(failedRequests) / float64(totalRequests) * 100
	avgLatency := time.Duration(0)
	if len(latencies) > 0 {
		avgLatency = totalLatency / time.Duration(len(latencies))
	}

	duration := time.Since(startTime)
	rps := float64(successRequests) / duration.Seconds()

	cpuPercent, memoryMB := b.collectSystemMetrics()

	return BenchmarkResult{
		Scenario:        scenario,
		Protocol:        protocol,
		Endpoint:        endpoint,
		TotalRequests:   totalRequests,
		SuccessRequests: successRequests,
		FailedRequests:  failedRequests,
		ErrorRate:       errorRate,
		Duration:        duration,
		AvgLatency:      avgLatency,
		P50Latency:      calculatePercentile(latencies, 50),
		P95Latency:      calculatePercentile(latencies, 95),
		P99Latency:      calculatePercentile(latencies, 99),
		RPS:             rps,
		CPUPercent:      cpuPercent,
		MemoryMB:        memoryMB,
		BytesIn:         totalBytesIn,
		BytesOut:        totalBytesOut,
		ConcurrentUsers: concurrentUsers,
	}
}

func (b *Benchmarker) calculateBytesFromResponse(resp interface{}, err error, isGRPC bool) int64 {
	if resp != nil {
		if isGRPC {
			if protoMsg, ok := resp.(proto.Message); ok {
				if data, marshalErr := proto.Marshal(protoMsg); marshalErr == nil {
					return int64(len(data))
				}
			}

			if data, marshalErr := json.Marshal(resp); marshalErr == nil && len(data) > 0 {
				return int64(len(data))
			}
		} else {
			if data, marshalErr := json.Marshal(resp); marshalErr == nil && len(data) > 0 {
				return int64(len(data))
			}
		}
	}

	if err != nil && isGRPC {
		return int64(len(err.Error()) + 20)
	}

	log.Printf("WARNING: Unable to calculate bytes dynamically for response: %+v, error: %v", resp, err)

	return 0
}

func (b *Benchmarker) calculateHTTPRequestBytes(method, url string, body []byte) int64 {
	bytes := int64(len(fmt.Sprintf("%s %s HTTP/1.1\r\n", method, url)))
	bytes += int64(len(fmt.Sprintf("Host: %s\r\n", b.config.RESTAddress)))
	bytes += int64(len("User-Agent: Go-http-client/1.1\r\n"))
	bytes += int64(len("Accept-Encoding: gzip\r\n"))
	if body != nil {
		bytes += int64(len("Content-Type: application/json\r\n"))
		bytes += int64(len(fmt.Sprintf("Content-Length: %d\r\n", len(body))))
		bytes += int64(len(body))
	}
	return bytes + 2
}

func (b *Benchmarker) calculateHTTPResponseBytes(resp *http.Response) int64 {
	if resp == nil {
		return 0
	}

	headerBytes := int64(len(fmt.Sprintf("%s %s\r\n", resp.Proto, resp.Status)))
	for name, values := range resp.Header {
		for _, value := range values {
			headerBytes += int64(len(fmt.Sprintf("%s: %s\r\n", name, value)))
		}
	}
	headerBytes += 2

	if resp.Body != nil {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			return headerBytes + int64(len(bodyBytes))
		}
		resp.Body.Close()
	}

	return headerBytes
}

func (b *Benchmarker) benchmarkGRPCGetUser(ctx context.Context, userID int32) RequestMetrics {
	start := time.Now()

	req := &pb.GetUserRequest{Id: userID}
	resp, err := b.grpcClient.GetUser(ctx, req)

	latency := time.Since(start)

	success := (err == nil && resp != nil) ||
		(err != nil && strings.Contains(err.Error(), "NotFound"))

	reqData, _ := proto.Marshal(req)
	bytesOut := int64(len(reqData))

	bytesIn := b.calculateBytesFromResponse(resp, err, true)

	return RequestMetrics{
		Latency:   latency,
		Success:   success,
		BytesIn:   bytesIn,
		BytesOut:  bytesOut,
		Timestamp: start,
	}
}

func (b *Benchmarker) benchmarkRESTGetUser(ctx context.Context, userID int32) RequestMetrics {
	start := time.Now()

	url := fmt.Sprintf("http://%s/users/%d", b.config.RESTAddress, userID)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)

	bytesOut := b.calculateHTTPRequestBytes("GET", url, nil)

	resp, err := b.httpClient.Do(req)
	success := err == nil && resp != nil && resp.StatusCode == http.StatusOK

	bytesIn := b.calculateHTTPResponseBytes(resp)

	latency := time.Since(start)

	return RequestMetrics{
		Latency:   latency,
		Success:   success,
		BytesIn:   bytesIn,
		BytesOut:  bytesOut,
		Timestamp: start,
	}
}

func (b *Benchmarker) benchmarkGRPCGetUsers(ctx context.Context, limit, offset int32) RequestMetrics {
	start := time.Now()

	req := &pb.GetUsersRequest{Limit: limit, Offset: offset}
	resp, err := b.grpcClient.GetUsers(ctx, req)

	latency := time.Since(start)
	success := err == nil && resp != nil

	reqData, _ := proto.Marshal(req)
	bytesOut := int64(len(reqData))
	bytesIn := b.calculateBytesFromResponse(resp, err, true)

	return RequestMetrics{
		Latency:   latency,
		Success:   success,
		BytesIn:   bytesIn,
		BytesOut:  bytesOut,
		Timestamp: start,
	}
}

func (b *Benchmarker) benchmarkRESTGetUsers(ctx context.Context, limit, offset int32) RequestMetrics {
	start := time.Now()

	url := fmt.Sprintf("http://%s/users?limit=%d&offset=%d", b.config.RESTAddress, limit, offset)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)

	bytesOut := b.calculateHTTPRequestBytes("GET", url, nil)

	resp, err := b.httpClient.Do(req)
	success := err == nil && resp != nil && resp.StatusCode == http.StatusOK

	bytesIn := b.calculateHTTPResponseBytes(resp)

	latency := time.Since(start)

	return RequestMetrics{
		Latency:   latency,
		Success:   success,
		BytesIn:   bytesIn,
		BytesOut:  bytesOut,
		Timestamp: start,
	}
}

func (b *Benchmarker) benchmarkGRPCCreateUser(ctx context.Context, user *TestUser) RequestMetrics {
	start := time.Now()

	req := &pb.CreateUserRequest{
		Name:       user.Name,
		Email:      user.Email,
		Age:        user.Age,
		Address:    user.Address,
		Phone:      user.Phone,
		Company:    user.Company,
		Department: user.Department,
		Position:   user.Position,
		Skills:     user.Skills,
	}

	resp, err := b.grpcClient.CreateUser(ctx, req)

	latency := time.Since(start)

	success := err == nil && resp != nil ||
		(err != nil && strings.Contains(err.Error(), "InvalidArgument"))

	reqData, _ := proto.Marshal(req)
	bytesOut := int64(len(reqData))
	bytesIn := b.calculateBytesFromResponse(resp, err, true)

	return RequestMetrics{
		Latency:   latency,
		Success:   success,
		BytesIn:   bytesIn,
		BytesOut:  bytesOut,
		Timestamp: start,
	}
}

func (b *Benchmarker) benchmarkRESTCreateUser(ctx context.Context, user *TestUser) RequestMetrics {
	start := time.Now()

	jsonData, _ := json.Marshal(user)
	url := fmt.Sprintf("http://%s/users", b.config.RESTAddress)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	bytesOut := b.calculateHTTPRequestBytes("POST", url, jsonData)

	resp, err := b.httpClient.Do(req)
	success := err == nil && resp != nil && resp.StatusCode == http.StatusCreated

	bytesIn := b.calculateHTTPResponseBytes(resp)

	latency := time.Since(start)

	return RequestMetrics{
		Latency:   latency,
		Success:   success,
		BytesIn:   bytesIn,
		BytesOut:  bytesOut,
		Timestamp: start,
	}
}

func (b *Benchmarker) executeRequest(protocol Protocol, endpoint EndpointType, ctx context.Context,
	testUsers []*TestUser, userIndex, requestIndex int) RequestMetrics {

	switch {
	case protocol == GRPC && endpoint == GetUser:
		return b.benchmarkGRPCGetUser(ctx, int32((userIndex%5)+1))
	case protocol == REST && endpoint == GetUser:
		return b.benchmarkRESTGetUser(ctx, int32((userIndex%5)+1))
	case protocol == GRPC && endpoint == GetUsers:
		return b.benchmarkGRPCGetUsers(ctx, 10, int32(userIndex%3))
	case protocol == REST && endpoint == GetUsers:
		return b.benchmarkRESTGetUsers(ctx, 10, int32(userIndex%3))
	case protocol == GRPC && endpoint == CreateUser:
		testUser := testUsers[userIndex%len(testUsers)]
		testUser.Email = fmt.Sprintf("user%d_%d@example.com", userIndex, requestIndex)
		return b.benchmarkGRPCCreateUser(ctx, testUser)
	case protocol == REST && endpoint == CreateUser:
		testUser := testUsers[userIndex%len(testUsers)]
		testUser.Email = fmt.Sprintf("user%d_%d@example.com", userIndex, requestIndex)
		return b.benchmarkRESTCreateUser(ctx, testUser)
	}
	return RequestMetrics{}
}

func (b *Benchmarker) runConcurrentTest(scenario TestScenario, protocol Protocol, endpoint EndpointType,
	concurrentUsers int, requestsPerUser int, payloadSize string) BenchmarkResult {

	log.Printf("Running %s test: %s %s with %d concurrent users, %d requests each",
		scenario, protocol, endpoint, concurrentUsers, requestsPerUser)

	var wg sync.WaitGroup
	metricsChan := make(chan RequestMetrics, concurrentUsers*requestsPerUser)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	startTime := time.Now()

	testUsers := b.generateTestUsers(payloadSize)

	for i := 0; i < concurrentUsers; i++ {
		wg.Add(1)
		go func(userIndex int) {
			defer wg.Done()

			for j := 0; j < requestsPerUser; j++ {
				metric := b.executeRequest(protocol, endpoint, ctx, testUsers, userIndex, j)
				metricsChan <- metric
			}
		}(i)
	}

	wg.Wait()
	close(metricsChan)

	metrics := make([]RequestMetrics, 0, concurrentUsers*requestsPerUser)
	for metric := range metricsChan {
		metrics = append(metrics, metric)
	}

	result := b.processResults(scenario, protocol, endpoint, metrics, concurrentUsers, startTime)

	avgBytesIn := float64(result.BytesIn) / float64(result.TotalRequests)
	avgBytesOut := float64(result.BytesOut) / float64(result.TotalRequests)

	b.mu.Lock()
	b.results = append(b.results, result)
	b.mu.Unlock()

	log.Printf("Completed %s test: %s %s - RPS: %.2f, Avg Latency: %v, Error Rate: %.2f%%, Avg Bytes: In=%.1f, Out=%.1f",
		scenario, protocol, endpoint, result.RPS, result.AvgLatency, result.ErrorRate, avgBytesIn, avgBytesOut)

	return result
}

func (b *Benchmarker) runSustainedTest(protocol Protocol, endpoint EndpointType) BenchmarkResult {
	log.Printf("Running sustained load test: %s %s (50 RPS for 60 seconds)", protocol, endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Second / 50)
	defer ticker.Stop()

	var requestCount int64
	metricsChan := make(chan RequestMetrics, 3000)
	startTime := time.Now()

	testUsers := b.generateTestUsers("small")

	var wg sync.WaitGroup

	go func() {
		defer func() {
			wg.Wait()
			close(metricsChan)
		}()

		for {
			select {
			case <-ticker.C:
				wg.Add(1)
				go func() {
					defer wg.Done()
					count := atomic.AddInt64(&requestCount, 1)
					metric := b.executeRequest(protocol, endpoint, ctx, testUsers, int(count), int(count))

					select {
					case metricsChan <- metric:
					case <-ctx.Done():
						return
					}
				}()
			case <-ctx.Done():
				return
			}
		}
	}()

	time.Sleep(60 * time.Second)
	cancel()

	metrics := make([]RequestMetrics, 0)
	for metric := range metricsChan {
		metrics = append(metrics, metric)
	}

	result := b.processResults(SustainedLoad, protocol, endpoint, metrics, 1, startTime)

	avgBytesIn := float64(result.BytesIn) / float64(result.TotalRequests)
	avgBytesOut := float64(result.BytesOut) / float64(result.TotalRequests)

	b.mu.Lock()
	b.results = append(b.results, result)
	b.mu.Unlock()

	log.Printf("Completed sustained load test: %s %s - RPS: %.2f, Avg Latency: %v, Error Rate: %.2f%%, Avg Bytes: In=%.1f, Out=%.1f",
		protocol, endpoint, result.RPS, result.AvgLatency, result.ErrorRate, avgBytesIn, avgBytesOut)

	return result
}

func (b *Benchmarker) RunAllBenchmarks() error {
	log.Println("Starting comprehensive benchmark suite...")

	smallUsers := b.generateTestUsers("small")
	largeUsers := b.generateTestUsers("large")

	smallData, _ := json.Marshal(smallUsers[0])
	largeData, _ := json.Marshal(largeUsers[0])

	log.Printf("Payload sizes: Small=%d bytes (~%.1fKB), Large=%d bytes (~%.1fKB)",
		len(smallData), float64(len(smallData))/1024,
		len(largeData), float64(len(largeData))/1024)

	log.Println("Warming up services...")
	b.runConcurrentTest("Warmup", GRPC, GetUser, 2, 5, "small")
	b.runConcurrentTest("Warmup", REST, GetUser, 2, 5, "small")

	log.Println("Waiting for system stabilization...")
	time.Sleep(10 * time.Second)

	endpoints := []EndpointType{GetUser, GetUsers, CreateUser}
	protocols := []Protocol{GRPC, REST}

	log.Println("\n=== SMALL PAYLOAD TESTS ===")
	for _, protocol := range protocols {
		for _, endpoint := range endpoints {
			b.runConcurrentTest(SmallPayload, protocol, endpoint, 10, 10, "small")
			time.Sleep(5 * time.Second)
		}
	}

	log.Println("\n=== LARGE PAYLOAD TESTS ===")
	for _, protocol := range protocols {
		for _, endpoint := range endpoints {
			b.runConcurrentTest(LargePayload, protocol, endpoint, 10, 10, "large")
			time.Sleep(5 * time.Second)
		}
	}

	log.Println("\n=== CONCURRENT LOAD TESTS ===")
	concurrentUsers := []int{10, 50, 100}
	for _, users := range concurrentUsers {
		log.Printf("Testing with %d concurrent users...", users)
		for _, protocol := range protocols {
			for _, endpoint := range endpoints {
				b.runConcurrentTest(ConcurrentLoad, protocol, endpoint, users, 10, "small")
				time.Sleep(5 * time.Second)
			}
		}
	}

	log.Println("\n=== BURST LOAD TESTS ===")
	for _, protocol := range protocols {
		for _, endpoint := range endpoints {
			b.runConcurrentTest(BurstLoad, protocol, endpoint, 100, 10, "small")
			time.Sleep(10 * time.Second)
		}
	}

	log.Println("\n=== SUSTAINED LOAD TESTS ===")
	for _, protocol := range protocols {
		for _, endpoint := range endpoints {
			b.runSustainedTest(protocol, endpoint)
			time.Sleep(10 * time.Second)
		}
	}

	log.Println("All benchmarks completed successfully!")
	return nil
}

func (b *Benchmarker) ExportResults() error {
	timestamp := time.Now().Format("20060102_1504")
	filename := fmt.Sprintf("results/benchmark_%s.csv", timestamp)

	if err := os.MkdirAll("results", 0755); err != nil {
		return fmt.Errorf("failed to create results directory: %v", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{
		"Scenario", "Protocol", "Endpoint", "TotalRequests", "SuccessRequests", "FailedRequests",
		"ErrorRate", "DurationMs", "AvgLatencyMs", "P50LatencyMs", "P95LatencyMs", "P99LatencyMs",
		"RPS", "CPUPercent", "MemoryMB", "BytesIn", "BytesOut", "ConcurrentUsers",
	}
	writer.Write(header)

	b.mu.RLock()
	for _, result := range b.results {
		row := []string{
			string(result.Scenario),
			string(result.Protocol),
			string(result.Endpoint),
			strconv.FormatInt(result.TotalRequests, 10),
			strconv.FormatInt(result.SuccessRequests, 10),
			strconv.FormatInt(result.FailedRequests, 10),
			fmt.Sprintf("%.2f", result.ErrorRate),
			strconv.FormatInt(result.Duration.Milliseconds(), 10),
			strconv.FormatInt(result.AvgLatency.Milliseconds(), 10),
			strconv.FormatInt(result.P50Latency.Milliseconds(), 10),
			strconv.FormatInt(result.P95Latency.Milliseconds(), 10),
			strconv.FormatInt(result.P99Latency.Milliseconds(), 10),
			fmt.Sprintf("%.2f", result.RPS),
			fmt.Sprintf("%.2f", result.CPUPercent),
			fmt.Sprintf("%.2f", result.MemoryMB),
			strconv.FormatInt(result.BytesIn, 10),
			strconv.FormatInt(result.BytesOut, 10),
			strconv.Itoa(result.ConcurrentUsers),
		}
		writer.Write(row)
	}
	b.mu.RUnlock()

	log.Printf("Benchmark results exported to: %s", filename)
	return nil
}

func (b *Benchmarker) PrintSummary() {
	log.Println("\n=== BENCHMARK SUMMARY ===")

	b.mu.RLock()
	grpcResults := make([]BenchmarkResult, 0)
	restResults := make([]BenchmarkResult, 0)

	for _, result := range b.results {
		if result.Protocol == GRPC {
			grpcResults = append(grpcResults, result)
		} else {
			restResults = append(restResults, result)
		}
	}
	b.mu.RUnlock()

	if len(grpcResults) > 0 {
		avgGRPCLatency := time.Duration(0)
		avgGRPCRPS := 0.0
		for _, result := range grpcResults {
			avgGRPCLatency += result.AvgLatency
			avgGRPCRPS += result.RPS
		}
		avgGRPCLatency /= time.Duration(len(grpcResults))
		avgGRPCRPS /= float64(len(grpcResults))

		log.Printf("gRPC Average - Latency: %v, RPS: %.2f", avgGRPCLatency, avgGRPCRPS)
	}

	if len(restResults) > 0 {
		avgRESTLatency := time.Duration(0)
		avgRESTRPS := 0.0
		for _, result := range restResults {
			avgRESTLatency += result.AvgLatency
			avgRESTRPS += result.RPS
		}
		avgRESTLatency /= time.Duration(len(restResults))
		avgRESTRPS /= float64(len(restResults))

		log.Printf("REST Average - Latency: %v, RPS: %.2f", avgRESTLatency, avgRESTRPS)
	}

	log.Printf("Total test results: %d", len(b.results))
	log.Println("=========================")
}

func (b *Benchmarker) validateResults() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, result := range b.results {
		if result.Scenario == "Warmup" {
			continue
		}

		if result.Protocol == GRPC && result.BytesIn == 0 && result.SuccessRequests > 50 {
			log.Printf("WARNING: gRPC %s %s has BytesIn=0 with %d successful requests",
				result.Scenario, result.Endpoint, result.SuccessRequests)
		}

		avgBytesPerReq := float64(result.BytesIn) / float64(result.TotalRequests)
		if result.Protocol == GRPC && avgBytesPerReq > 0 && avgBytesPerReq < 10 {
			log.Printf("WARNING: gRPC %s %s has unusually low avg bytes per request: %.1f",
				result.Scenario, result.Endpoint, avgBytesPerReq)
		}

		if result.Protocol == REST && avgBytesPerReq > 10000 {
			log.Printf("WARNING: REST %s %s has unusually high avg bytes per request: %.1f",
				result.Scenario, result.Endpoint, avgBytesPerReq)
		}

		if result.AvgLatency < 5*time.Millisecond {
			log.Printf("WARNING: Suspiciously low latency %v for %s %s",
				result.AvgLatency, result.Protocol, result.Endpoint)
		}

		if result.AvgLatency > 5*time.Second {
			return fmt.Errorf("ERROR: Too high latency %v for %s %s",
				result.AvgLatency, result.Protocol, result.Endpoint)
		}

		if result.ErrorRate > 50 {
			return fmt.Errorf("ERROR: High error rate %.2f%% for %s %s",
				result.ErrorRate, result.Protocol, result.Endpoint)
		}

		if result.RPS > 0 && result.AvgLatency > 0 {
			maxTheoreticalRPS := float64(result.ConcurrentUsers) / result.AvgLatency.Seconds()
			if result.RPS > maxTheoreticalRPS*2 {
				log.Printf("WARNING: RPS %.2f is suspiciously high for latency %v and %d users",
					result.RPS, result.AvgLatency, result.ConcurrentUsers)
			}
		}
	}

	return nil
}

func main() {
	rand.Seed(42)

	log.Println("gRPC vs REST Academic Benchmarking Tool")
	log.Println("=======================================")
	log.Printf("Start time: %s", time.Now().Format("2006-01-02 15:04:05"))

	config := BenchmarkConfig{
		GRPCAddress:  "localhost:50051",
		RESTAddress:  "localhost:8080",
		TestDuration: 60 * time.Second,
		WarmupTime:   5 * time.Second,
	}

	benchmarker, err := NewBenchmarker(config)
	if err != nil {
		log.Fatalf("Failed to create benchmarker: %v", err)
	}
	defer benchmarker.Close()

	log.Println("Waiting for services to be ready...")
	time.Sleep(2 * time.Second)

	log.Println("Testing service connectivity...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = benchmarker.grpcClient.GetUser(ctx, &pb.GetUserRequest{Id: 1})
	if err != nil && !strings.Contains(err.Error(), "NotFound") && !strings.Contains(err.Error(), "not found") {
		log.Fatalf("❌ gRPC service not reachable at %s: %v", config.GRPCAddress, err)
	}
	log.Printf("✓ gRPC service accessible at %s", config.GRPCAddress)

	resp, err := benchmarker.httpClient.Get(fmt.Sprintf("http://%s/users/1", config.RESTAddress))
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		log.Fatalf("❌ REST service not reachable at %s: %v", config.RESTAddress, err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	log.Printf("✓ REST service accessible at %s", config.RESTAddress)

	if err := benchmarker.RunAllBenchmarks(); err != nil {
		log.Fatalf("Benchmark execution failed: %v", err)
	}

	if err := benchmarker.validateResults(); err != nil {
		log.Fatalf("Validation failed: %v", err)
	}

	if err := benchmarker.ExportResults(); err != nil {
		log.Fatalf("Failed to export results: %v", err)
	}

	benchmarker.PrintSummary()

	log.Println("Benchmarking completed successfully!")
	log.Println("Results have been saved to the results/ directory in CSV format.")
	log.Println("You can now analyze the data for your academic research.")
	log.Printf("End time: %s", time.Now().Format("2006-01-02 15:04:05"))
}
