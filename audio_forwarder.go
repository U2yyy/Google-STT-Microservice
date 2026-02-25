package main

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/speech/apiv2/speechpb"
)

const defaultStreamChunkBytes = 25600

type audioForwarder struct {
	stream      speechpb.Speech_StreamingRecognizeClient
	audioIn     <-chan []byte
	stopSignal  <-chan struct{}
	logger      *slog.Logger
	vad         *EnergyVAD
	sendBuf     []byte
	chunkBytes  int
	inBytes     int64
	sentBytes   int64
	suppressed  int64
	streamEnded bool
}

func newAudioForwarder(
	stream speechpb.Speech_StreamingRecognizeClient,
	audioIn <-chan []byte,
	stopSignal <-chan struct{},
	logger *slog.Logger,
	vad *EnergyVAD,
) *audioForwarder {
	return &audioForwarder{
		stream:     stream,
		audioIn:    audioIn,
		stopSignal: stopSignal,
		logger:     logger,
		vad:        vad,
		sendBuf:    make([]byte, 0, defaultStreamChunkBytes*2),
		chunkBytes: defaultStreamChunkBytes,
	}
}

func (f *audioForwarder) run(ctx context.Context) {
	defer func() {
		f.vad.Flush()
		if err := f.flushAndClose(); err != nil {
			f.logger.Info("flushAndClose failed", "error", err)
		}
		f.suppressed = f.inBytes - f.sentBytes
		if f.suppressed < 0 {
			f.suppressed = 0
		}
		f.logger.Info(
			"audio forwarding finished",
			"inputBytes", f.inBytes,
			"sentBytes", f.sentBytes,
			"suppressedBytes", f.suppressed,
		)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.stopSignal:
			return
		case payload, ok := <-f.audioIn:
			if !ok {
				return
			}
			f.inBytes += int64(len(payload))
			if err := f.handleInputChunk(payload); err != nil {
				f.logger.Info("audio forwarding send failed", "error", err)
				return
			}
		}
	}
}

func (f *audioForwarder) handleInputChunk(payload []byte) error {
	voiced := f.vad.Filter(payload)
	if len(voiced) == 0 {
		return nil
	}

	f.sendBuf = append(f.sendBuf, voiced...)
	for len(f.sendBuf) >= f.chunkBytes {
		if err := f.sendAudio(f.sendBuf[:f.chunkBytes]); err != nil {
			return err
		}
		f.sendBuf = f.sendBuf[f.chunkBytes:]
	}
	return nil
}

func (f *audioForwarder) flushAndClose() error {
	var firstErr error

	for len(f.sendBuf) > 0 {
		sendSize := f.chunkBytes
		if len(f.sendBuf) < sendSize {
			sendSize = len(f.sendBuf)
		}
		if err := f.sendAudio(f.sendBuf[:sendSize]); err != nil && firstErr == nil {
			firstErr = err
			break
		}
		f.sendBuf = f.sendBuf[sendSize:]
	}

	if !f.streamEnded {
		if err := f.stream.CloseSend(); err != nil && firstErr == nil {
			firstErr = err
		}
		f.streamEnded = true
	}

	return firstErr
}

func (f *audioForwarder) sendAudio(audio []byte) error {
	if len(audio) == 0 {
		return nil
	}
	err := f.stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{
			Audio: audio,
		},
	})
	if err != nil {
		return fmt.Errorf("stream.Send audio failed: %w", err)
	}
	f.sentBytes += int64(len(audio))
	return nil
}
