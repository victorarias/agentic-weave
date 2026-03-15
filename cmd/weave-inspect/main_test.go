package main

import (
	"io"
	"testing"
	"time"
)

func TestReadTakeoverInputDetectsSplitCSIUCtrlRightBracket(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()

	out := make(chan takeoverInputEvent, 16)
	errCh := make(chan error, 1)
	go readTakeoverInput(reader, out, errCh, nil)

	sequence := []byte{0x1b, '[', '9', '3', ';', '5', 'u'}
	for _, b := range sequence {
		if _, err := writer.Write([]byte{b}); err != nil {
			t.Fatalf("write byte %x: %v", b, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = writer.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
	case evt, ok := <-out:
		if !ok {
			t.Fatal("output channel closed before event")
		}
		if !evt.disconnect {
			t.Fatalf("expected disconnect event, got %#v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for disconnect event")
	}
}

func TestReadTakeoverInputDetectsSplitModifyOtherKeysCtrlRightBracket(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()

	out := make(chan takeoverInputEvent, 16)
	errCh := make(chan error, 1)
	go readTakeoverInput(reader, out, errCh, nil)

	sequence := []byte{0x1b, '[', '2', '7', ';', '5', ';', '9', '3', '~'}
	for _, b := range sequence {
		if _, err := writer.Write([]byte{b}); err != nil {
			t.Fatalf("write byte %x: %v", b, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = writer.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
	case evt, ok := <-out:
		if !ok {
			t.Fatal("output channel closed before event")
		}
		if !evt.disconnect {
			t.Fatalf("expected disconnect event, got %#v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for disconnect event")
	}
}
