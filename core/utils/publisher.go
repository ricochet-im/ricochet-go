package utils

type Publisher struct {
	subscribeChannel   chan chan interface{}
	unsubscribeChannel chan (<-chan interface{})
	broadcastChannel   chan interface{}
	closeChannel       chan struct{}
	closedChannel      chan struct{}
}

type Subscribable interface {
	Subscribe(queueSize int) <-chan interface{}
	Unsubscribe(channel <-chan interface{})
}

func CreatePublisher() *Publisher {
	re := &Publisher{
		subscribeChannel:   make(chan chan interface{}),
		unsubscribeChannel: make(chan (<-chan interface{})),
		broadcastChannel:   make(chan interface{}),
		closeChannel:       make(chan struct{}),
		closedChannel:      make(chan struct{}),
	}
	go re.broadcast()
	return re
}

func (pub *Publisher) Subscribe(queueSize int) <-chan interface{} {
	channel := make(chan interface{}, queueSize)
	pub.subscribeChannel <- channel
	// Read empty value to block until subscribed
	<-channel
	return channel
}

func (pub *Publisher) Unsubscribe(channel <-chan interface{}) {
	pub.unsubscribeChannel <- channel
	// Wait for channel close
	for {
		_, ok := <-channel
		if !ok {
			return
		}
	}
}

func (pub *Publisher) Publish(value interface{}) {
	pub.broadcastChannel <- value
}

func (pub *Publisher) Close() {
	pub.closeChannel <- struct{}{}
	<-pub.closedChannel

	// Close channels, so that future use of the object will panic
	close(pub.subscribeChannel)
	close(pub.unsubscribeChannel)
	close(pub.broadcastChannel)
	close(pub.closeChannel)
	close(pub.closedChannel)
}

func (pub *Publisher) broadcast() {
	var channels []chan interface{}

	for {
		select {
		case channel := <-pub.subscribeChannel:
			channels = append(channels, channel)
			channel <- struct{}{}
		case channel := <-pub.unsubscribeChannel:
			for i, c := range channels {
				if c != channel {
					continue
				}
				channels = append(channels[:i], channels[i+1:]...)
				close(c)
				break
			}
		case value := <-pub.broadcastChannel:
			for _, c := range channels {
				select {
				case c <- value:
				default:
					go pub.Unsubscribe(c)
				}
			}
		case <-pub.closeChannel:
			for _, c := range channels {
				close(c)
			}
			pub.closedChannel <- struct{}{}
			return
		}
	}
}
