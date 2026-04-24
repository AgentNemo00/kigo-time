package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AgentNemo00/kigo-core/inquiry"
	"github.com/AgentNemo00/kigo-core/notification"
	"github.com/AgentNemo00/kigo-core/order"
	"github.com/AgentNemo00/kigo-core/ui"
	"github.com/AgentNemo00/sca-instruments/containerization"
	"github.com/AgentNemo00/sca-instruments/log"
	ps "github.com/AgentNemo00/sca-instruments/pubsub"
	"github.com/AgentNemo00/sca-instruments/pubsub/nats"
	"github.com/AgentNemo00/sca-instruments/security"
)

const (
	url = "nats://127.0.0.1:4222"
)

func main() {
	// startup durations start
	start := time.Now()

	// pubsub publisher
	pub, err := nats.PublisherWithURL[notification.Notification](url)
	if err != nil {
		log.Ctx(context.Background()).Err(err)
		return
	}
	
	// pubsub suscriber
	sub, err := nats.SubscriberWithURL[order.Order](url)
	if err != nil {
		log.Ctx(context.Background()).Err(err)
		return
	}

	// generate uuid to be able to get startup order
	uuid, err := security.UUID()
	if err != nil {
		log.Ctx(context.Background()).Err(err)
		return
	}

	
	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error)
	dataChan := make(chan order.OrderStartUpPayload)
	
	// subscribe to temporary uuid to be able go get OrderStartUp
	subscription, err := sub.Subscribe(ctx, uuid, func(ctx context.Context, metadata ps.Metadata, data *order.Order) {
		if metadata.Error != nil {
			log.Ctx(ctx).Err(err)
			return
		} 
		// anything else is an error
		if data.Order != order.OrderStartUp{
			errChan <- fmt.Errorf("wrong order: %s", data.Order)
			return
		}
		// convert
		var payload order.OrderStartUpPayload
		err := mapToStruct(data.Payload, &payload)
		if err != nil {
			errChan <- err
		}
		dataChan <- payload
	})
	if err != nil {
		log.Ctx(context.Background()).Err(err)
		return
	}

	containerization.Callback(func ()  {
		subscription.Unsubscribe(ctx)
		close(dataChan)
		cancel()
	})

	go containerization.Interrupt(func() {})

	// shutdown if error during ready handshake
	go func ()  {
		value, ok := <-errChan
		if ok {
			log.Ctx(context.Background()).Err(value)
			log.Ctx(context.Background()).Info("context canceled")
			cancel()
		}
	}()

	// singalize ready
	go func ()  {
		err := pub.Publish(ctx, "KiGo", notification.Notification{
			From: uuid,
			To: "KiGo",
			Notification: notification.NotificationReady,
			Payload: notification.NotificationReadyPayload{
				Name: "Time",
				Changes: []string{"ChangeFormat"},
				Heartbeat: time.Minute*2,
				Duration: time.Now().Sub(start),
			},
		})		
		if err != nil {
			errChan <- err
		}
	}()
	
	log.Ctx(context.Background()).Info("waiting for startup routine")
	
	var renderTo string	
	l: for {
		select {
		case <- ctx.Done():
			return
		case startUpPayload := <- dataChan:
			uuid = startUpPayload.ID
			renderTo = startUpPayload.MessageTo.Render
			subscription.Unsubscribe(ctx)
			break l
		default:
		}
	}

	log.Ctx(context.Background()).Info("creating render")
	
	subscription, err = sub.Subscribe(ctx, uuid, func(ctx context.Context, metadata ps.Metadata, data *order.Order)  {
		if metadata.Error != nil {
			log.Ctx(ctx).Err(err)
			return
		}
		if data.Order != order.OrderRender {
			errChan <- fmt.Errorf("wrong order: %s", data.Order)
			return
		}
		fmt.Println(data)
	})
	if err != nil {
		log.Ctx(ctx).Err(err)
		return
	}

	log.Ctx(context.Background()).Info("inquiry Notification")
	
	err = pub.Publish(ctx, renderTo, notification.Notification{
		From: uuid,
		To: renderTo,
		Notification: inquiry.InquiryRender,
		Payload: inquiry.InquiryRenderPayload{
			Format: ui.RAW,
			Channel: ui.IPC,
			FPS: 20,
			MaxFrameSize: 100,
			Timeout: time.Millisecond*16,
		},
	})
	if err != nil {
		log.Ctx(ctx).Err(err)
		return
	}
	for {
		select {
			case <- ctx.Done():
				return
			default:
		}
	}
}

func mapToStruct(m any, out any) error {
    data, err := json.Marshal(m)
    if err != nil {
        return err
    }
    return json.Unmarshal(data, out)
}