package price

import "fmt"

// Mock base prices per asset (deterministic for offline testing).
var mockBasePrices = map[string]float64{
	"BTC/USD": 95000.0,
	"ETH/USD": 3500.0,
	"SOL/USD": 150.0,
}

// MockFetcher returns a deterministic price per asset with a small percentage
// offset so the three instances (mock1/mock2/mock3) simulate different oracle
// sources that agree approximately but not exactly.
type MockFetcher struct {
	name            string
	percentOffset   float64 // e.g. 0.002 = +0.2%
}

func NewMock1() *MockFetcher { return &MockFetcher{name: "mock1", percentOffset: 0} }
func NewMock2() *MockFetcher { return &MockFetcher{name: "mock2", percentOffset: 0.002} }   // +0.2%
func NewMock3() *MockFetcher { return &MockFetcher{name: "mock3", percentOffset: -0.0015} } // -0.15%

func (m *MockFetcher) Name() string { return m.name }

func (m *MockFetcher) FetchPrice(asset string) (float64, error) {
	base, ok := mockBasePrices[asset]
	if !ok {
		return 0, fmt.Errorf("mock: unsupported asset %q (supported: BTC/USD, ETH/USD, SOL/USD)", asset)
	}
	return base * (1 + m.percentOffset), nil
}
