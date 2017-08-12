package policies

import (
	"github.com/s-rah/go-ricochet/utils"
	"time"
)

// TimeoutPolicy is a convieance interface for enforcing common timeout patterns
type TimeoutPolicy time.Duration

// Selection of common timeout policies
const (
	UnknownPurposeTimeout TimeoutPolicy = TimeoutPolicy(15 * time.Second)
)

// ExecuteAction runs a function and returns an error if it hasn't returned
// by the time specified by TimeoutPolicy
func (tp *TimeoutPolicy) ExecuteAction(action func() error) error {

	c := make(chan error)
	go func() {
		c <- action()
	}()

	tick := time.Tick(time.Duration(*tp))
	select {
	case <-tick:
		return utils.ActionTimedOutError
	case err := <-c:
		return err
	}
}
