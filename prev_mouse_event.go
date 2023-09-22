package main

import (
	proto "github.com/Nekhaevalex/fwsprotocol"
	"github.com/nsf/termbox-go"
)

type prevMouseEvent struct {
	key            termbox.Key
	layer          proto.ID
	owner          pid
	layerX, layerY int
}

func (ev prevMouseEvent) sameLayer(new termbox.Event) bool {
	prevCont := (ev.key == termbox.MouseLeft) || (ev.key == termbox.MouseMiddle) || (ev.key == termbox.MouseRight)
	newCont := (new.Key == termbox.MouseLeft) || (new.Key == termbox.MouseMiddle) || (new.Key == termbox.MouseRight) || (new.Key == termbox.MouseRelease)
	return prevCont && newCont
}

func (ev prevMouseEvent) getPlaneLayerPos(x, y int) (pid, proto.ID, int, int) {
	return ev.owner, ev.layer, (x - ev.layerX), (y - ev.layerY)
}

func (ev *prevMouseEvent) saveState(owner pid, event termbox.Event, d_event proto.EventRequest) {
	ev.key = event.Key
	ev.layer = d_event.Id
	ev.owner = owner
	ev.layerX = event.MouseX - d_event.MouseX
	ev.layerY = event.MouseY - d_event.MouseY
}
