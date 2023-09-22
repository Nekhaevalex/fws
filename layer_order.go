package main

import (
	"container/list"

	proto "github.com/Nekhaevalex/fwsprotocol"
)

// Structure providing layer management
type LayerOrder struct {
	layers *list.List
	index  map[proto.ID]*list.Element
}

// Inits new LayerOrder
func (queue *LayerOrder) init() {
	queue.layers = list.New()
	queue.index = make(map[proto.ID]*list.Element)
}

// Pushes new layer
func (queue *LayerOrder) push(layer *Layer) {
	if queue.layers != nil {
		lid := layer.id
		var new_element *list.Element
		switch layer.attribute {
		case proto.BOTTOM:
			new_element = queue.layers.PushBack(layer)
		default:
			new_element = queue.layers.PushFront(layer)
		}
		queue.index[lid] = new_element
	}

}

// Returns pointer to layer
func (queue *LayerOrder) get(id proto.ID) *Layer {
	return queue.index[id].Value.(*Layer)
}

// Moves layer with ID to top
func (queue *LayerOrder) top(id proto.ID) {
	switch queue.get(id).attribute {
	case proto.BOTTOM:
		queue.layers.MoveToBack(queue.index[id])
	default:
		queue.layers.MoveToFront(queue.index[id])
	}
}
