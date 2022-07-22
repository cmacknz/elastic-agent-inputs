// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package cursor

import (
	"time"

	"github.com/elastic/elastic-agent-inputs/pkg/manager/input"
	"github.com/elastic/elastic-agent-inputs/pkg/publisher"
	"github.com/elastic/elastic-agent-inputs/pkg/statestore"
	"github.com/elastic/elastic-agent-libs/transform/typeconv"
)

// Publisher is used to publish an event and update the cursor in a single call to Publish.
// Inputs are allowed to pass `nil` as cursor state. In this case the state is not updated, but the
// event will still be published as is.
type Publisher interface {
	Publish(event publisher.Event, cursor interface{}) error
}

// cursorPublisher implements the Publisher interface and used internally by the managedInput.
// When publishing an event with cursor state updates, the cursorPublisher
// updates the in memory state and create an updateOp that is used to schedule
// an update for the persistent store. The updateOp is run by the inputs ACK
// handler, persisting the pending update.
type cursorPublisher struct {
	canceler input.Canceler
	client   publisher.Client
	cursor   *Cursor
}

// updateOp keeps track of pending updates that are not written to the persistent store yet.
// Update operations are ordered. The input manager guarantees that only one
// input can create update operation for a source, such that new input
// instances can add update operations to be executed after already pending
// update operations from older inputs instances that have been shutdown.
type updateOp struct {
	store    *store
	resource *resource

	// state updates to persist
	timestamp time.Time
	delta     interface{}
}

// Publish publishes an event. Publish returns false if the inputs cancellation context has been marked as done.
// If cursorUpdate is not nil, Publish updates the in memory state and create and updateOp for the pending update.
// It overwrite event.Private with the update operation, before finally sending the event.
// The ACK ordering in the publisher pipeline guarantees that update operations
// will be ACKed and executed in the correct order.
func (c *cursorPublisher) Publish(event publisher.Event, cursorUpdate interface{}) error {
	if cursorUpdate == nil {
		return c.forward(event)
	}

	op, err := createUpdateOp(c.cursor.store, c.cursor.resource, cursorUpdate)
	if err != nil {
		return err
	}

	event.Private = op
	return c.forward(event)
}

func (c *cursorPublisher) forward(event publisher.Event) error {
	c.client.Publish(event)
	if c.canceler == nil {
		return nil
	}
	return c.canceler.Err()
}

func createUpdateOp(store *store, resource *resource, updates interface{}) (*updateOp, error) {
	ts := time.Now()

	resource.stateMutex.Lock()
	defer resource.stateMutex.Unlock()

	cursor := resource.pendingCursor
	if resource.activeCursorOperations == 0 {
		var tmp interface{}
		_ = typeconv.Convert(&tmp, cursor)
		resource.pendingCursor = tmp
		cursor = tmp
	}
	if err := typeconv.Convert(&cursor, updates); err != nil {
		return nil, err
	}
	resource.pendingCursor = cursor

	resource.Retain()
	resource.activeCursorOperations++
	return &updateOp{
		resource:  resource,
		store:     store,
		timestamp: ts,
		delta:     updates,
	}, nil
}

// done releases resources held by the last N updateOps.
func (op *updateOp) done(n uint) {
	op.resource.UpdatesReleaseN(n)
	op.resource = nil
	*op = updateOp{}
}

// Execute updates the persistent store with the scheduled changes and releases the resource.
func (op *updateOp) Execute(n uint) {
	resource := op.resource
	defer op.done(n)

	resource.stateMutex.Lock()
	defer resource.stateMutex.Unlock()

	resource.activeCursorOperations -= n
	if resource.activeCursorOperations == 0 {
		resource.cursor = resource.pendingCursor
		resource.pendingCursor = nil
	} else {
		_ = typeconv.Convert(&resource.cursor, op.delta)
	}

	if resource.internalState.Updated.Before(op.timestamp) {
		resource.internalState.Updated = op.timestamp
	}

	err := op.store.persistentStore.Set(resource.key, resource.inSyncStateSnapshot())
	if err != nil {
		if !statestore.IsClosed(err) {
			op.store.log.Errorf("Failed to update state in the registry for '%v'", resource.key)
		}
	} else {
		resource.internalInSync = true
		resource.stored = true
	}
}
