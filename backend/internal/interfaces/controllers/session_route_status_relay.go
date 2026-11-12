package controllers

import (
	"context"
	"log"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	routeStatusOutboxSize         = 256
	routeStatusPublishTimeout     = 3 * time.Second
	routeStatusResubscribeBackoff = 3 * time.Second
)

func WithRouteStatusEventBus(bus repositories.RouteStatusEventBus) SessionControllerOption {
	return func(c *SessionController) { c.routeStatusBus = bus }
}

func (c *SessionController) NotifyRouteStatus(evt repositories.SessionStatusEvent) {
	c.publishRemoteStatusEvent(evt)
}

func (c *SessionController) StartRouteStatusRelay(ctx context.Context) {
	if c.routeStatusBus == nil {
		return
	}
	go c.runRouteStatusPublisher(ctx)
	go c.runRouteStatusSubscriber(ctx)
}

func (c *SessionController) enqueueRouteStatusEvent(evt repositories.SessionStatusEvent) {
	if c.routeStatusBus == nil {
		return
	}
	select {
	case c.routeStatusOutbox <- evt:
	default:
		log.Printf("[ROUTE_STATUS] Outbox full, dropping cross-replica event session=%s status=%s", evt.SessionID, evt.Status)
	}
}

func (c *SessionController) runRouteStatusPublisher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-c.routeStatusOutbox:
			publishCtx, cancel := context.WithTimeout(ctx, routeStatusPublishTimeout)
			err := c.routeStatusBus.PublishRouteStatusChange(publishCtx, repositories.StatusChangeEvent{
				SessionID: evt.SessionID,
				Status:    evt.Status,
				UpdatedAt: evt.Timestamp,
				PodID:     c.routeStatusOrigin,
			})
			cancel()
			if err != nil {
				log.Printf("[ROUTE_STATUS] Failed to publish session=%s status=%s: %v", evt.SessionID, evt.Status, err)
			}
		}
	}
}

func (c *SessionController) runRouteStatusSubscriber(ctx context.Context) {
	for ctx.Err() == nil {
		ch, err := c.routeStatusBus.SubscribeRouteStatus(ctx)
		if err != nil {
			log.Printf("[ROUTE_STATUS] Failed to subscribe, retrying in %s: %v", routeStatusResubscribeBackoff, err)
		} else {
			for evt := range ch {
				if evt.PodID == c.routeStatusOrigin {
					continue
				}
				c.deliverRemoteStatusEventLocal(repositories.SessionStatusEvent{
					SessionID: evt.SessionID,
					Status:    evt.Status,
					Timestamp: evt.UpdatedAt,
				})
			}
		}
		timer := time.NewTimer(routeStatusResubscribeBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
