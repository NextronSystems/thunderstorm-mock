package thunderstormmock

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/go-faker/faker/v4"
)

type ScanID int64

var currentID ScanID

func init() {
	currentID = ScanID(rand.Int63n(1024) + 42)
}

var idMutex sync.Mutex

func NextScanID() ScanID {
	idMutex.Lock()
	defer idMutex.Unlock()
	currentID++
	return currentID
}

var scanRequests sync.Map

type ScanRequest struct {
	ID             ScanID
	Synchronous    bool
	FileHash       string
	Source         string
	SubmissionTime time.Time
}

func (s ScanRequest) ToThorFinding() ThorFinding {
	return ThorFinding{
		"type":   "THOR Finding",
		"id":     int64(s.ID),
		"hash":   s.FileHash,
		"source": s.Source,
	}
}

func (s ScanRequest) ToThorReport() ThorReport {
	return ThorReport{s.ToThorFinding()}
}

const (
	asyncWaitingTime  = 2 * time.Second
	asyncProgressTime = 5 * time.Second
)

// ToResult converts the ScanRequest to a result. For convenience of use and to gather logic in one place, it works with synchronous requests, too, and simply (mis)uses an AsyncResult as a return type. If the scan ID is 0, it returns a failure status.
func (s ScanRequest) ToResult() AsyncResult {
	if s.ID == 0 {
		return AsyncResult{
			Status: crashed.String(),
		}
	}

	if s.Synchronous {
		report := s.ToThorReport()
		return AsyncResult{
			Status: finished.String(),
			Result: &report,
		}
	}

	age := time.Since(s.SubmissionTime)
	switch {
	case age < asyncWaitingTime:
		return AsyncResult{
			Status: waiting.String(),
		}
	case age < asyncProgressTime:
		return AsyncResult{
			Status: inProgress.String(),
		}
	default:
		report := s.ToThorReport()
		return AsyncResult{
			Status: finished.String(),
			Result: &report,
		}
	}
}

func StoreScanRequest(synchronous bool, file io.Reader, source string) (ScanRequest, error) {
	submissionTime := time.Now()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return ScanRequest{}, err
	}
	fileHash := fmt.Sprintf("%x", h.Sum(nil))

	scanID := NextScanID()

	scanRequest := ScanRequest{
		ID:             scanID,
		Synchronous:    synchronous,
		FileHash:       fileHash,
		Source:         source,
		SubmissionTime: submissionTime,
	}

	scanRequests.Store(scanID, scanRequest)
	return scanRequest, nil
}

func LoadScanRequest(id int64) (ScanRequest, bool) {
	if req, found := scanRequests.Load(ScanID(id)); found {
		return req.(ScanRequest), true
	}
	return ScanRequest{}, false
}

const historyTimeLayout = "2006-01-02 15:04"

type historyType string

const (
	sampleHistoryType historyType = "samples"
	queueHistoryType  historyType = "queue"
)

func History(ht historyType, aggregateMinutes, limitMinutes int64) TimestampMap {
	if aggregateMinutes < 1 {
		aggregateMinutes = 1
	}
	if limitMinutes <= 0 {
		// The default maximum is some very long time ago. The value is not limited
		// by the minimal time.Time value but by the time.Duration limit (int64 in
		// nanoseconds, i.e., about 290 years) where limitMinutes is used in.
		limitMinutes = 1 << 27
	}

	timesByTime := map[string]int64{}
	bucketDuration := time.Minute * time.Duration(aggregateMinutes)
	now := time.Now()
	minObservedTime := now
	scanRequests.Range(func(key, value any) bool {
		req := value.(ScanRequest)
		bucketStart := req.SubmissionTime.Truncate(bucketDuration)
		if bucketStart.Before(minObservedTime) {
			minObservedTime = bucketStart
		}
		if ht == queueHistoryType {
			if req.SubmissionTime.Add(asyncWaitingTime).Before(bucketStart.Add(bucketDuration)) {
				return true
			}
		}
		bucketString := bucketStart.Format(historyTimeLayout)
		timesByTime[bucketString]++
		return true
	})

	history := map[string]int64{}
	oldestTime := minObservedTime
	if limitTime := now.Truncate(time.Minute).Add(time.Minute * time.Duration(-1*limitMinutes+1)).Truncate(bucketDuration); limitTime.After(oldestTime) {
		oldestTime = limitTime
	}
	for t := oldestTime; t.Before(now); t = t.Add(bucketDuration) {
		bucketStr := t.Format(historyTimeLayout)
		history[bucketStr] = timesByTime[bucketStr]
	}
	return history
}

func MockInfo() ThunderstormInfo {
	var ti ThunderstormInfo
	_ = faker.FakeData(&ti)
	return ti
}

func MockStatus() ThunderstormStatus {
	var scannedSamples int64
	var queuedRequests int64
	var totalScanTime time.Duration
	var totalWaitTime time.Duration

	scanRequests.Range(func(key, value any) bool {
		req := value.(ScanRequest)
		age := time.Since(req.SubmissionTime)

		if req.Synchronous {
			scannedSamples++
			totalScanTime += time.Duration(42+rand.Intn(500)) * time.Millisecond
		} else {
			result := req.ToResult()
			switch result.Status {
			case waiting.String():
				queuedRequests++
				totalWaitTime += age
			case finished.String():
				scannedSamples++
				totalScanTime += time.Duration(42+rand.Intn(500)) * time.Millisecond
			}
		}
		return true
	})

	var avgScanTimeMs int64
	if scannedSamples > 0 {
		avgScanTimeMs = totalScanTime.Milliseconds() / scannedSamples
	}

	var avgWaitTimeMs int64
	if queuedRequests > 0 {
		avgWaitTimeMs = totalWaitTime.Milliseconds() / queuedRequests
	}

	return ThunderstormStatus{
		ScannedSamples:          scannedSamples,
		QueuedAsyncRequests:     queuedRequests,
		AvgScanTimeMilliseconds: avgScanTimeMs,
		AvgWaitTimeMilliseconds: avgWaitTimeMs,
	}
}
