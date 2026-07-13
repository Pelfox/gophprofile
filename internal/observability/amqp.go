package observability

import amqp "github.com/rabbitmq/amqp091-go"

type AMQPHeaderCarrier amqp.Table

func (c AMQPHeaderCarrier) Get(key string) string {
	value, ok := c[key]
	if !ok {
		return ""
	}
	val, ok := value.(string)
	if ok {
		return val
	}
	return ""
}

func (c AMQPHeaderCarrier) Set(key string, value string) {
	c[key] = value
}

func (c AMQPHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}
