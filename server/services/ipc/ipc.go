// Package ipc provides utilities for parent-child process communication
// using FlatBuffers for efficient binary serialization.
package ipc

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/samyak-jain/agora_backend/services/ipc/botipc"
	flatbuffers "github.com/google/flatbuffers/go"
)

// MaxMessageSize is the maximum allowed message size (10MB)
const MaxMessageSize = 10 * 1024 * 1024

// MessageWriter handles writing length-prefixed FlatBuffer messages
type MessageWriter struct {
	writer *bufio.Writer
	mu     sync.Mutex
}

// NewMessageWriter creates a new MessageWriter
func NewMessageWriter(w io.Writer) *MessageWriter {
	return &MessageWriter{
		writer: bufio.NewWriter(w),
	}
}

// WriteMessage writes a length-prefixed FlatBuffer message
// Format: [4 bytes big-endian length][payload bytes]
func (mw *MessageWriter) WriteMessage(data []byte) error {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	// Write 4-byte length prefix (big-endian)
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len(data)))

	if _, err := mw.writer.Write(lenBytes); err != nil {
		return fmt.Errorf("failed to write message length: %w", err)
	}

	if _, err := mw.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write message payload: %w", err)
	}

	if err := mw.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush message: %w", err)
	}

	return nil
}

// MessageReader handles reading length-prefixed FlatBuffer messages
type MessageReader struct {
	reader *bufio.Reader
}

// NewMessageReader creates a new MessageReader
func NewMessageReader(r io.Reader) *MessageReader {
	return &MessageReader{
		reader: bufio.NewReader(r),
	}
}

// ReadMessage reads a length-prefixed FlatBuffer message
// Returns the raw bytes which can be parsed with GetRootAsIPCMessage
func (mr *MessageReader) ReadMessage() ([]byte, error) {
	// Read 4-byte length prefix
	lenBytes := make([]byte, 4)
	if _, err := io.ReadFull(mr.reader, lenBytes); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read message length: %w", err)
	}

	msgLen := binary.BigEndian.Uint32(lenBytes)
	if msgLen == 0 {
		return nil, fmt.Errorf("received zero-length message")
	}
	if msgLen > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", msgLen, MaxMessageSize)
	}

	// Read message payload
	msgBuf := make([]byte, msgLen)
	if _, err := io.ReadFull(mr.reader, msgBuf); err != nil {
		return nil, fmt.Errorf("failed to read message payload: %w", err)
	}

	return msgBuf, nil
}

// Helper functions to build common messages

// BuildStartSessionMessage creates a START_SESSION message
func BuildStartSessionMessage(
	taskID, appID, channel string,
	botUID uint32, botToken string,
	palabraUID uint32,
	anamAPIKey, anamBaseURL, anamAvatarID string,
	anamUID uint32, anamToken string,
	targetLanguage string,
) []byte {
	// Build the StartSessionPayload
	innerBuilder := flatbuffers.NewBuilder(1024)

	taskIDOffset := innerBuilder.CreateString(taskID)
	appIDOffset := innerBuilder.CreateString(appID)
	channelOffset := innerBuilder.CreateString(channel)
	botTokenOffset := innerBuilder.CreateString(botToken)
	anamAPIKeyOffset := innerBuilder.CreateString(anamAPIKey)
	anamBaseURLOffset := innerBuilder.CreateString(anamBaseURL)
	anamAvatarIDOffset := innerBuilder.CreateString(anamAvatarID)
	anamTokenOffset := innerBuilder.CreateString(anamToken)
	targetLangOffset := innerBuilder.CreateString(targetLanguage)

	botipc.StartSessionPayloadStart(innerBuilder)
	botipc.StartSessionPayloadAddTaskId(innerBuilder, taskIDOffset)
	botipc.StartSessionPayloadAddAppId(innerBuilder, appIDOffset)
	botipc.StartSessionPayloadAddChannel(innerBuilder, channelOffset)
	botipc.StartSessionPayloadAddBotUid(innerBuilder, botUID)
	botipc.StartSessionPayloadAddBotToken(innerBuilder, botTokenOffset)
	botipc.StartSessionPayloadAddPalabraUid(innerBuilder, palabraUID)
	botipc.StartSessionPayloadAddAnamApiKey(innerBuilder, anamAPIKeyOffset)
	botipc.StartSessionPayloadAddAnamBaseUrl(innerBuilder, anamBaseURLOffset)
	botipc.StartSessionPayloadAddAnamAvatarId(innerBuilder, anamAvatarIDOffset)
	botipc.StartSessionPayloadAddAnamUid(innerBuilder, anamUID)
	botipc.StartSessionPayloadAddAnamToken(innerBuilder, anamTokenOffset)
	botipc.StartSessionPayloadAddTargetLanguage(innerBuilder, targetLangOffset)
	payloadOffset := botipc.StartSessionPayloadEnd(innerBuilder)
	innerBuilder.Finish(payloadOffset)
	payloadBytes := innerBuilder.FinishedBytes()

	// Build the IPCMessage wrapper
	return buildIPCMessage(botipc.MessageTypeSTART_SESSION, payloadBytes)
}

// BuildStopSessionMessage creates a STOP_SESSION message
func BuildStopSessionMessage(taskID, reason string) []byte {
	innerBuilder := flatbuffers.NewBuilder(256)

	taskIDOffset := innerBuilder.CreateString(taskID)
	reasonOffset := innerBuilder.CreateString(reason)

	botipc.StopSessionPayloadStart(innerBuilder)
	botipc.StopSessionPayloadAddTaskId(innerBuilder, taskIDOffset)
	botipc.StopSessionPayloadAddReason(innerBuilder, reasonOffset)
	payloadOffset := botipc.StopSessionPayloadEnd(innerBuilder)
	innerBuilder.Finish(payloadOffset)
	payloadBytes := innerBuilder.FinishedBytes()

	return buildIPCMessage(botipc.MessageTypeSTOP_SESSION, payloadBytes)
}

// BuildStatusMessage creates a STATUS_UPDATE message
func BuildStatusMessage(taskID string, status botipc.SessionStatus, message string, anamUID uint32) []byte {
	innerBuilder := flatbuffers.NewBuilder(256)

	taskIDOffset := innerBuilder.CreateString(taskID)
	messageOffset := innerBuilder.CreateString(message)

	botipc.StatusPayloadStart(innerBuilder)
	botipc.StatusPayloadAddTaskId(innerBuilder, taskIDOffset)
	botipc.StatusPayloadAddStatus(innerBuilder, status)
	botipc.StatusPayloadAddMessage(innerBuilder, messageOffset)
	botipc.StatusPayloadAddAnamUid(innerBuilder, anamUID)
	payloadOffset := botipc.StatusPayloadEnd(innerBuilder)
	innerBuilder.Finish(payloadOffset)
	payloadBytes := innerBuilder.FinishedBytes()

	return buildIPCMessage(botipc.MessageTypeSTATUS_UPDATE, payloadBytes)
}

// BuildLogMessage creates a LOG_MESSAGE message
func BuildLogMessage(taskID string, level botipc.LogLevel, message string) []byte {
	innerBuilder := flatbuffers.NewBuilder(512)

	taskIDOffset := innerBuilder.CreateString(taskID)
	messageOffset := innerBuilder.CreateString(message)

	botipc.LogPayloadStart(innerBuilder)
	botipc.LogPayloadAddTaskId(innerBuilder, taskIDOffset)
	botipc.LogPayloadAddLevel(innerBuilder, level)
	botipc.LogPayloadAddMessage(innerBuilder, messageOffset)
	payloadOffset := botipc.LogPayloadEnd(innerBuilder)
	innerBuilder.Finish(payloadOffset)
	payloadBytes := innerBuilder.FinishedBytes()

	return buildIPCMessage(botipc.MessageTypeLOG_MESSAGE, payloadBytes)
}

// BuildErrorMessage creates an ERROR_RESPONSE message
func BuildErrorMessage(taskID, errorCode, message string, fatal bool) []byte {
	innerBuilder := flatbuffers.NewBuilder(512)

	taskIDOffset := innerBuilder.CreateString(taskID)
	errorCodeOffset := innerBuilder.CreateString(errorCode)
	messageOffset := innerBuilder.CreateString(message)

	botipc.ErrorPayloadStart(innerBuilder)
	botipc.ErrorPayloadAddTaskId(innerBuilder, taskIDOffset)
	botipc.ErrorPayloadAddErrorCode(innerBuilder, errorCodeOffset)
	botipc.ErrorPayloadAddMessage(innerBuilder, messageOffset)
	botipc.ErrorPayloadAddFatal(innerBuilder, fatal)
	payloadOffset := botipc.ErrorPayloadEnd(innerBuilder)
	innerBuilder.Finish(payloadOffset)
	payloadBytes := innerBuilder.FinishedBytes()

	return buildIPCMessage(botipc.MessageTypeERROR_RESPONSE, payloadBytes)
}

// buildIPCMessage wraps a payload in an IPCMessage
func buildIPCMessage(msgType botipc.MessageType, payloadBytes []byte) []byte {
	builder := flatbuffers.NewBuilder(len(payloadBytes) + 64)

	// Create payload vector
	botipc.IPCMessageStartPayloadVector(builder, len(payloadBytes))
	for i := len(payloadBytes) - 1; i >= 0; i-- {
		builder.PrependByte(payloadBytes[i])
	}
	payloadOffset := builder.EndVector(len(payloadBytes))

	// Create IPCMessage
	botipc.IPCMessageStart(builder)
	botipc.IPCMessageAddMessageType(builder, msgType)
	botipc.IPCMessageAddPayload(builder, payloadOffset)
	msg := botipc.IPCMessageEnd(builder)
	builder.Finish(msg)

	return builder.FinishedBytes()
}

// ParseIPCMessage parses an IPCMessage and returns the type and payload bytes
func ParseIPCMessage(data []byte) (botipc.MessageType, []byte, error) {
	msg := botipc.GetRootAsIPCMessage(data, 0)

	payloadLen := msg.PayloadLength()
	payloadBytes := make([]byte, payloadLen)
	for i := 0; i < payloadLen; i++ {
		payloadBytes[i] = byte(msg.Payload(i))
	}

	return msg.MessageType(), payloadBytes, nil
}

// ParseStartSessionPayload parses a StartSessionPayload from bytes
func ParseStartSessionPayload(data []byte) *botipc.StartSessionPayload {
	return botipc.GetRootAsStartSessionPayload(data, 0)
}

// ParseStopSessionPayload parses a StopSessionPayload from bytes
func ParseStopSessionPayload(data []byte) *botipc.StopSessionPayload {
	return botipc.GetRootAsStopSessionPayload(data, 0)
}

// ParseStatusPayload parses a StatusPayload from bytes
func ParseStatusPayload(data []byte) *botipc.StatusPayload {
	return botipc.GetRootAsStatusPayload(data, 0)
}

// ParseLogPayload parses a LogPayload from bytes
func ParseLogPayload(data []byte) *botipc.LogPayload {
	return botipc.GetRootAsLogPayload(data, 0)
}

// ParseErrorPayload parses an ErrorPayload from bytes
func ParseErrorPayload(data []byte) *botipc.ErrorPayload {
	return botipc.GetRootAsErrorPayload(data, 0)
}
