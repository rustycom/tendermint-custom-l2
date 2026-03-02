package price

import "fmt"

const mockBasePrice = 95000.0

// MockFetcher returns a deterministic price derived from a base price plus a
// fixed offset. Three instances (mock1/mock2/mock3) simulate different oracle
// sources that agree approximately but not exactly.
type MockFetcher struct {
	name   string
	offset float64
}

func NewMock1() *MockFetcher { return &MockFetcher{name: "mock1", offset: 0} }
func NewMock2() *MockFetcher { return &MockFetcher{name: "mock2", offset: 150} }
func NewMock3() *MockFetcher { return &MockFetcher{name: "mock3", offset: -120} }

func (m *MockFetcher) Name() string { return m.name }

func (m *MockFetcher) FetchPrice(asset string) (float64, error) {
	if asset != "BTC/USD" {
		return 0, fmt.Errorf("mock: unsupported asset %q (only BTC/USD)", asset)
	}
	return mockBasePrice + m.offset, nil
}
