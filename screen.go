package main

import (
	proto "github.com/Nekhaevalex/fwsprotocol"
	"github.com/Nekhaevalex/queue"
	"github.com/nsf/termbox-go"
)

// Screen abstraction handler.
type Screen struct {
	id            int                             // screen ID
	width, height int                             // screen size
	objects       LayerOrder                      // layers on the screen
	selected      proto.ID                        // currently selected layer id
	globalEvents  chan termbox.Event              // global events incomming channel
	replyChannels map[pid]chan proto.Request      // reply channels
	eventChannels map[pid]chan proto.EventRequest // dispatched events channels
	prevMouseKey  prevMouseEvent                  // previous pressed mouse event (for window movements)
	requestQueue  *queue.ChannelQueue[proto.Msg]  // requests queue
	outputMode    termbox.OutputMode              // Screen output mode
	quit          chan int                        // quit message channel
}

// Register app handler
func (screen *Screen) registerApp(pid pid) (
	eventChanel chan proto.EventRequest,
	replyChanel chan proto.Request,
	quit chan int) {
	return screen.registerEventChannel(pid),
		screen.registerReplyChannel(pid),
		screen.quit
}

// Issue new layer ID
func (screen *Screen) issueNewLayerID() proto.ID {
	return proto.ID(screen.objects.layers.Len())
}

// Registers new event channel for events dispatching
func (screen *Screen) registerEventChannel(pid pid) chan proto.EventRequest {
	screen.eventChannels[pid] = make(chan proto.EventRequest)
	return screen.eventChannels[pid]
}

// Registers new reply channel for events dispatching
func (screen *Screen) registerReplyChannel(pid pid) chan proto.Request {
	screen.replyChannels[pid] = make(chan proto.Request)
	return screen.replyChannels[pid]
}

func (screen *Screen) render() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	for elem := screen.objects.layers.Back(); elem != nil; elem = elem.Prev() {
		layer := elem.Value.(*Layer)
		if layer.render {
			for lx := 0; lx < layer.width; lx++ {
				for ly := 0; ly < layer.height; ly++ {
					rx := lx + layer.x
					ry := ly + layer.y

					if rx < 0 || rx >= screen.width {
						continue
					}

					if ry < 0 || ry >= screen.height {
						continue
					}

					tbCell := termbox.GetCell(rx, ry)
					pCell := proto.FromTermboxCell(tbCell)
					over := layer.canvas[lx][ly]
					new := over.Over(pCell)
					tbNew := new.ToTerboxCell()
					termbox.SetCell(rx, ry, tbNew.Ch, tbNew.Fg, tbNew.Bg)
				}
			}
		}
	}
	termbox.Flush()
}

func (screen *Screen) enqueueRequest(request proto.Msg) {
	screen.requestQueue.Enqueue(request)
}

func (screen *Screen) globalEventProcessor(eventsQueue *queue.ChannelQueue[termbox.Event]) {
	for {
		select {
		case <-screen.quit:
			return
		case globalEvent := <-eventsQueue.Dequeue():
			pid, dispatched_event := screen.dispatchEvent(globalEvent)
			switch pid {
			case -1:
				// Empty event
			case 0:
				// Send to all apps
				for pid := range screen.eventChannels {
					screen.eventChannels[pid] <- dispatched_event
				}
			default:
				if dispatched_event.Id != screen.selected {
					focus := proto.FocusRequest{Id: dispatched_event.Id}
					screen.enqueueRequest(focus.Encode())
				}
				screen.eventChannels[pid] <- dispatched_event
			}
		}
	}
}

// Screen handling loop. Start via go operator
func (screen *Screen) handle() {
	// start globalEvents_reciever
	go screen.globalEventsReceiver()
	// start render worker
	go screen.renderWorker()
	// make new queue for global event processor
	globalEventQueue := queue.NewChannelQueue[termbox.Event](1024)
	go screen.globalEventProcessor(globalEventQueue)
	// handle requests & events
	for {
		select {
		case globalEvent := <-screen.globalEvents:
			globalEventQueue.Enqueue(globalEvent)
		case <-screen.quit:
			return
		}
	}
}

func (screen *Screen) renderWorker() {
	for {
		request := <-screen.requestQueue.Dequeue()
		screen.proceedRequest(request)
	}
}

func (screen *Screen) proceedRequest(request proto.Msg) {
	switch decoded := request.Decode().(type) {
	case *proto.NewWindowRequest:
		screen.implementNewWindow(decoded)
	case *proto.GetRequest:
		screen.implementGet(decoded)
	case *proto.DrawRequest:
		screen.implementDraw(decoded)
	case *proto.DrawFillRequest:
		screen.implementDrawFill(decoded)
	case *proto.RenderRequest:
		screen.implementRender(decoded)
	case *proto.DeleteRequest:
		screen.implementDeleteRequest(decoded)
	case *proto.ResizeRequest:
		screen.implementResize(decoded)
	case *proto.MoveRequest:
		screen.implementMove(decoded)
	case *proto.FocusRequest:
		screen.implementFocus(decoded)
	case *proto.ScreenRequest:
		screen.implementScreenRequest(decoded)
	}
}

func (screen *Screen) implementNewWindow(decoded *proto.NewWindowRequest) {
	lId := screen.issueNewLayerID()
	layer := newLayer(lId, *decoded)
	screen.objects.push(layer)
	reply := &proto.ReplyCreationRequest{Id: lId}
	screen.replyChannels[pid(decoded.Pid)] <- reply
	screen.selected = lId
}

func (screen *Screen) implementGet(decoded *proto.GetRequest) {
	lid := decoded.Id
	x := decoded.X
	y := decoded.Y
	cell := screen.objects.get(lid).canvas[x][y]
	reply := &proto.ReplyGetRequest{C: cell}
	pid := screen.objects.get(lid).owner
	screen.replyChannels[pid] <- reply
}

func (screen *Screen) implementRender(decoded *proto.RenderRequest) {
	lid := decoded.Id
	screen.objects.get(lid).render = true
	screen.render()
}

func (screen *Screen) implementDraw(decoded *proto.DrawRequest) {
	lid := decoded.Id
	x := decoded.X
	y := decoded.Y
	cell := decoded.Cell
	screen.objects.get(lid).render = false
	old_cell := screen.objects.get(lid).canvas[x][y]
	new_cell := cell.Over(old_cell)
	screen.objects.get(lid).canvas[x][y] = new_cell
}

func (screen *Screen) implementFocus(decoded *proto.FocusRequest) {
	lid := decoded.Id
	screen.objects.top(lid)
	screen.selected = lid
	screen.render()
}

func (screen *Screen) implementScreenRequest(decoded *proto.ScreenRequest) {
	lid := decoded.Id
	reply := &proto.ReplyScreenRequest{
		Width:  int32(screen.width),
		Height: int32(screen.height),
		Mode:   screen.outputMode,
	}
	pid := screen.objects.get(lid).owner
	screen.replyChannels[pid] <- reply
}

func (screen *Screen) implementMove(decoded *proto.MoveRequest) {
	lid := decoded.Id
	x := decoded.X
	y := decoded.Y
	screen.objects.get(lid).render = false
	screen.objects.get(lid).x += x
	screen.objects.get(lid).y += y
}

func (screen *Screen) implementResize(decoded *proto.ResizeRequest) {
	lid := decoded.Id
	width := decoded.Width
	height := decoded.Height
	screen.objects.get(lid).render = false
	screen.objects.get(lid).makeCanvas(width, height)
}

func (screen *Screen) implementDeleteRequest(decoded *proto.DeleteRequest) {
	lid := decoded.Id
	for e := screen.objects.layers.Front(); e != nil; e = e.Next() {
		lr := e.Value.(*Layer)
		if lr.id == lid {
			screen.objects.layers.Remove(e)
			delete(screen.objects.index, lid)
			next := e.Next()
			if next != nil {
				screen.selected = next.Value.(*Layer).id
			}
		}
	}
	screen.render()
}

func (screen *Screen) implementDrawFill(decoded *proto.DrawFillRequest) {
	lid := decoded.Id
	w := decoded.Width
	h := decoded.Height
	img := decoded.Img
	screen.objects.get(lid).render = false
	for i := 0; i < w; i++ {
		for j := 0; j < h; j++ {
			screen.objects.get(lid).canvas[i][j].Ch = img[i][j].Ch
			screen.objects.get(lid).canvas[i][j].Fg = img[i][j].Fg
			screen.objects.get(lid).canvas[i][j].Bg = img[i][j].Bg
			screen.objects.get(lid).canvas[i][j].Attribute = img[i][j].Attribute
		}
	}
}

func (screen *Screen) globalEventsReceiver() {
	for {
		event := termbox.PollEvent()
		select {
		case screen.globalEvents <- event:
		case <-screen.quit:
			return
		}
	}
}

func (screen *Screen) dispatchEvent(event termbox.Event) (pid, proto.EventRequest) {
	switch event.Type {
	case termbox.EventResize:
		newWidth, newHeight := termbox.Size()
		screen.height = newHeight
		screen.width = newWidth
		// Send resize to all apps
		return 0, proto.EventRequest{Id: screen.selected, Event: event}
	case termbox.EventKey:
		if screen.selected != 0 {
			return screen.objects.get(screen.selected).owner, proto.EventRequest{Id: screen.selected, Event: event}
		}
		return -1, proto.EventRequest{}
	case termbox.EventMouse:
		// Search sender on screen
		layers := screen.objects.layers
		x, y := event.MouseX, event.MouseY
		look_up_area := func() (pid, proto.ID, int, int) {
			if screen.prevMouseKey.sameLayer(event) {
				return screen.prevMouseKey.getPlaneLayerPos(x, y)
			}
			for e := layers.Front(); e != nil; e = e.Next() {
				switch layer := e.Value.(type) {
				case *Layer:
					if x >= layer.x && x <= (layer.x+layer.width-1) && y >= layer.y && y <= (layer.y+layer.height-1) {
						return layer.owner, layer.id, (x - layer.x), (y - layer.y)
					}
				}
			}
			return -1, 0, 0, 0
		}
		owner, lid, newX, newY := look_up_area()

		dispatched := event
		dispatched.MouseX = newX
		dispatched.MouseY = newY
		request := proto.EventRequest{Id: lid, Event: dispatched}
		screen.prevMouseKey.saveState(owner, event, request)
		return owner, request
	case termbox.EventInterrupt:
		return 0, proto.EventRequest{Id: screen.selected, Event: event}
	case termbox.EventNone:
		return -1, proto.EventRequest{Id: screen.selected, Event: event}
	default:
		return -1, proto.EventRequest{}
	}
}

func NewScreen(id, width, height int, outputMode termbox.OutputMode) *Screen {
	screen := &Screen{id: 0, width: width, height: height}
	screen.objects = LayerOrder{}
	screen.objects.init()
	screen.globalEvents = make(chan termbox.Event)
	screen.quit = make(chan int)
	screen.replyChannels = make(map[pid]chan proto.Request)
	screen.eventChannels = make(map[pid]chan proto.EventRequest)
	screen.selected = 0
	screen.requestQueue = queue.NewChannelQueue[proto.Msg](100)
	screen.outputMode = outputMode
	return screen
}
