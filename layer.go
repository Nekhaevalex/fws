package main

import proto "github.com/Nekhaevalex/fwsprotocol"

// Object descripting layer on screen
type Layer struct {
	owner         pid
	id            proto.ID
	x, y          int
	width, height int
	render        bool
	attribute     proto.LayerAttribute
	canvas        [][]proto.Cell
}

// Layer constructor from issued layer ID and creation request
func newLayer(lID proto.ID, request proto.NewWindowRequest) *Layer {
	layer := new(Layer)
	layer.owner = pid(request.Pid)
	layer.id = lID
	layer.x = request.X
	layer.y = request.Y
	layer.render = false
	layer.attribute = request.LayerAttr
	layer.makeCanvas(request.Width, request.Height)
	return layer
}

// Method allocating canvas
func (layer *Layer) makeCanvas(width int, height int) {
	layer.width = width
	layer.height = height
	layer.canvas = make([][]proto.Cell, width)
	for i := range layer.canvas {
		layer.canvas[i] = make([]proto.Cell, height)
	}
}
