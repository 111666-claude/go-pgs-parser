package buffer

import (
	"errors"
)

type CompositeBufferReader interface {
	Add(buffer []byte)

	Length() int

	ReadBytes(count int) (BufferAdapter, error)
}

type compositeBufferReader struct {
	buffers []BufferAdapter
}

func NewCompositeBufferReader() CompositeBufferReader {
	return &compositeBufferReader{}
}

func (c *compositeBufferReader) Add(buffer []byte) {
	c.buffers = append(c.buffers, NewUint8ArrayBuffer(buffer))
}

func (c *compositeBufferReader) Length() int {
	length := 0
	for _, b := range c.buffers {
		length += b.Length()
	}
	return length
}

func (c *compositeBufferReader) ReadBytes(count int) (BufferAdapter, error) {
	if count == 0 {
		return NewCompositeBuffer([]BufferAdapter{}), nil
	}

	var chunks []BufferAdapter
	remaining := count

	for remaining > 0 {
		if len(c.buffers) == 0 {
			return nil, errors.New("trying to read more bytes than available")
		}

		head := c.buffers[0]

		if head.Length() <= remaining {
			// Consume the whole chunk. Build a fresh slice header so dropping
			// the front element never aliases a backing array still referenced
			// by chunks handed out earlier.
			chunks = append(chunks, head)
			remaining -= head.Length()
			c.buffers = append([]BufferAdapter{}, c.buffers[1:]...)
			continue
		}

		// Split the head: return its prefix, keep the suffix queued. Both
		// slices are independent views produced via SubArray.
		prefix := head.SubArray(0, remaining)
		suffix := head.SubArray(remaining, head.Length())
		chunks = append(chunks, prefix)
		c.buffers[0] = suffix
		remaining = 0
	}

	return NewCompositeBuffer(chunks), nil
}
