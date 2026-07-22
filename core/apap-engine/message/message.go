// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

//go:generate go run ../message-gen/gen_codes.go

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	log "github.com/sirupsen/logrus"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// unknownCatalogMessage is a dummy catalog message indicating there was a
// problem trying to find the message in the catalog. This is used as a
// fallback when the message cannot be found in the catalog for some reason.
var unknownCatalogMessage = &CatalogMessage{
	Code:        "engine.message.LOOKUP_FAILED",
	Severity:    SeverityError,
	Message:     "There was an issue finding this message.",
	Explanation: "An internal error occurred while trying to find the message with code '{code}'.",
	Advice:      "Contact Arm support.",
}

// MessageCode is used to identify specific messages in the message catalog.
// They are used to look up the message in the catalog and provide a consistent
// way to reference messages across the codebase. Each message code should be
// unique and descriptive of the message it represents. MessageCodes are a
// combination of a Domain and a Reason, separated by a dot.
type MessageCode = string

type Message interface {
	Error() string               // Displays the error message in a human-readable format
	Unwrap() error               // Returns the underlying cause of the message
	Domain() string              // Returns the domain of the message
	Reason() string              // Returns the reason for the message
	Code() MessageCode           // Calculates and returns the unique code for this message
	Metadata() map[string]string // Returns the metadata associated with the message
	Locale() string              // Returns the locale for this message
	GRPCInfo() GRPCInfo          // Returns the gRPC information for this message
}

type GRPCInfo struct {
	GRPCCode    codes.Code // The gRPC code for the error
	GRPCMessage string     // The gRPC message for the error
}

type MessageImpl struct {
	domain   string            // The domain of the message (e.g. engine.recipe.run)
	reason   string            // The reason for the message (e.g. DAEMON_NOT_RUNNING)
	cause    error             // The wrapped cause of the message (enables chaining)
	metadata map[string]string // Extra contextual information relating to the message
	locale   string            // The locale for this message (e.g. en-US)
	grpcInfo GRPCInfo          // gRPC information for this message (if originates from engine)
}

// Error returns a human-readable representation of the message.
func (m *MessageImpl) Error() string {
	s := fmt.Sprintf("%s.%s", m.domain, m.reason)
	if m.cause != nil {
		s = fmt.Sprintf("%s: %v", s, m.cause.Error())
	}
	return s
}

// Domain returns the domain for this message.
func (m *MessageImpl) Domain() string {
	return m.domain
}

// Reason returns the reason for this message.
func (m *MessageImpl) Reason() string {
	return m.reason
}

// Code returns the unique code for this message which can be used to perform a
// lookup in the message catalog. Code is a combination of the domain and reason,
// formatted as DOMAIN.REASON (e.g. engine.recipe.run.DAEMON_NOT_RUNNING).
func (m *MessageImpl) Code() MessageCode {
	return MessageCode(fmt.Sprintf("%s.%s", m.domain, m.reason))
}

// Metadata returns the extra information related to the message.
func (m *MessageImpl) Metadata() map[string]string {
	return m.metadata
}

// Locale returns the locale for this message.
func (m *MessageImpl) Locale() string {
	if m.locale == "" {
		return LocaleEnglish // Default to English if no locale is set
	}
	return m.locale
}

// GRPCCode returns the gRPC code for this message.
func (m *MessageImpl) GRPCCode() string {
	return m.grpcInfo.GRPCCode.String()
}

// GRPCMessage returns the gRPC message for ths message.
func (m *MessageImpl) GRPCMessage() string {
	return m.grpcInfo.GRPCMessage
}

// GRPCInfo returns the gRPC information for this message.
func (m *MessageImpl) GRPCInfo() GRPCInfo {
	return m.grpcInfo
}

// New constructs a new MessageImpl with the given code and metadata.
func New(code MessageCode) *MessageImpl {
	codeStr := code
	lastDot := strings.LastIndex(codeStr, ".")

	// Invalid code format, should always have a dot separating domain and reason
	if lastDot == -1 {
		return nil
	}

	return &MessageImpl{
		domain:   codeStr[:lastDot],
		reason:   codeStr[lastDot+1:],
		metadata: make(map[string]string),
		grpcInfo: GRPCInfo{}, // This gets filled in automatically by gRPC interceptors
		cause:    nil,
	}
}

// CodeExists checks if a given message code exists in the catalog corresponding to the specified locale.
func CodeExists(code MessageCode, locale string) bool {
	catalog, ok := CatalogByLocale[locale]
	if !ok {
		return false
	}
	_, exists := catalog[code]
	return exists
}

// Unwrap returns the wrapped cause of the message, to support error chaining.
func (m *MessageImpl) Unwrap() error {
	if m == nil {
		return nil
	}
	return m.cause
}

// Wrap constructs a new MessageImpl with a wrapped cause. The code is the new
// message code, and the cause is the error being wrapped. Use this when you
// want to create a new message (e.g. at a boundary) and want to attach the
// cause within it.
func Wrap(code MessageCode, cause error) *MessageImpl {
	m := New(code)

	if m != nil {
		m.cause = cause
	}

	return m
}

// WithCause returns a copy of the message that wraps the given cause. The
// cause is the error being wrapped. Use this when you want to add a cause to
// an existing message without changing its properties (e.g. code, metadata).
// It copies the original MessageImpl but adds the new cause.
func (m *MessageImpl) WithCause(cause error) *MessageImpl {
	if m == nil {
		return nil
	}

	cp := *m
	if cause != nil && cause.Error() != "" {
		cp.cause = cause
	}

	return &cp
}

// Join aggregates multiple causes under a single Message code. It creates a new
// MessageImpl with the given code and joins the causes using errors.Join. Use
// this when you want to create a new message with multiple underlying errors,
// e.g. during a series of parameter validation checks. It is similar to Wrap
// but for multiple causes.
func Join(code MessageCode, causes ...error) *MessageImpl {
	return Wrap(code, errors.Join(causes...))
}

// WithMetadata adds additional metadata to the message. This can be used to
// provide extra context to be inserted into the message, or information that
// might be useful for debugging. It deep copies the original Message but adds
// the new metadata.
func (m *MessageImpl) WithMetadata(newData map[string]string) *MessageImpl {
	if m == nil {
		return nil
	}

	// Copy the original metadata
	merged := make(map[string]string, len(m.metadata))
	maps.Copy(merged, m.metadata)

	// Copy newData into oldData (will overwrite existing keys)
	maps.Copy(merged, newData)

	// Return a new MessageImpl with the same values, but updated metadata
	cp := *m
	cp.metadata = merged
	return &cp
}

// Is is a helper function to compare two messages by their MessageCode.
func (m *MessageImpl) Is(target error) bool {
	if t, ok := target.(*MessageImpl); ok {
		return m.Code() == t.Code()
	}
	return false
}

// IsInfoOrWarning looks up a message in the catalog and returns true if the
// message severity is Info or Warning.
func (m *MessageImpl) IsInfoOrWarning() bool {
	catalogMsg := m.Lookup()
	if catalogMsg != nil {
		return catalogMsg.Severity == SeverityInfo || catalogMsg.Severity == SeverityWarning
	}
	return false
}

// LookupMessage looks up the message in the catalog based on the error. If the
// error is a MessageImpl, it retrieves the stock message from the catalog and
// uses string interpolation to insert any metadata. If the error is not a
// MessageImpl, it returns an unknown CatalogMessage.
func LookupMessage(err error) (*CatalogMessage, error) {
	if m := IsMessage(err); m != nil {
		catalogMsg := m.Lookup()
		if catalogMsg != nil {
			return catalogMsg.Interpolate(m.Metadata()), nil
		}
		// Include code of message we attempted to look up if we failed
		return unknownCatalogMessage.Interpolate(map[string]string{"code": m.Code()}), nil
	}

	// If the error is not a MessageImpl, we can't look it up in the catalog
	return unknownCatalogMessage.Interpolate(map[string]string{"code": "nil"}), err
}

// Lookup retrieves the details for the given message code in the specified
// locale and returns a CatalogMessage. If no CatalogMessage is found matching
// the given message code, nil is returned.
func (m *MessageImpl) Lookup() *CatalogMessage {
	messageCode := m.Code()
	locale := m.Locale()

	// Lookup the message in the catalog for the specified locale
	if catalog, ok := CatalogByLocale[locale]; ok {
		if msg, ok := catalog[messageCode]; ok {
			return &msg
		}
	}

	// If the message is not found in the specified locale, fall back to English
	if locale != LocaleEnglish {
		log.Warnf("Catalog message %s not found in locale %s, falling back to %s", messageCode, locale, LocaleEnglish)
		if en, ok := CatalogByLocale[LocaleEnglish]; ok {
			if fallbackMsg, ok := en[messageCode]; ok {
				return &fallbackMsg
			}
		}
	}

	// If we reach here, something has gone horribly wrong.
	return nil
}

// ToGRPCStatus converts a Message to a gRPC status with error details.
// This is used by the engine to convert a Message into a gRPC Status so that
// it can be returned to the client with structured error information.
func (m *MessageImpl) ToGRPCStatus() error {
	st := status.New(codes.FailedPrecondition, m.Error())

	errorInfo := &errdetails.ErrorInfo{
		Reason:   m.Reason(),
		Domain:   m.Domain(),
		Metadata: m.Metadata(),
	}

	// Include the locale hint for the client (since this is defined in the engine)
	localized := &errdetails.LocalizedMessage{
		Locale:  LocaleEnglish, // Hardcoded for now, will become dynamic when localization is implemented
		Message: "",            // localization is handled by the client
	}

	// Attach full lossless error chain detail as Any (recursive tree)
	chain, anyErr := anypb.New(BuildErrorChain(m))
	if anyErr != nil {
		// Just try to carry on without the chain if we fail to build it
		chain = nil
	}

	// Attach details to the status
	var stWithDetails *status.Status
	var err error

	if chain != nil {
		stWithDetails, err = st.WithDetails(errorInfo, localized, chain)
	} else {
		stWithDetails, err = st.WithDetails(errorInfo, localized)
	}

	if err != nil {
		// Just return original status if details can't be added
		return st.Err()
	}

	return stWithDetails.Err()
}

// IsMessage is a helper function that checks if the error implements
// MessageImpl. If so, it returns the MessageImpl, otherwise it returns nil.
func IsMessage(err error) *MessageImpl {
	var m *MessageImpl
	if errors.As(err, &m) {
		return m
	} else {
		return nil
	}
}

// AsGRPCStatus is a helper function that checks if the error implements
// MessageImpl. If so, it converts it to a gRPC status so that it can be sent
// back to the client via gRPC. If not, it returns a generic gRPC status with
// the error message. This is used by engine APIs to report errors to clients.
func AsGRPCStatus(err error) error {
	if m := IsMessage(err); m != nil {
		return m.ToGRPCStatus()
	}

	// If the error isn't a MessageImpl, return as-is
	return err
}

// FromGRPCStatus converts a gRPC status with error details back into a
// MessageImpl so that it can be handled by the CLI client.
func FromGRPCStatus(err error) error {
	if err == nil {
		return nil
	}

	// If the error is already a MessageImpl, return it as is
	if m := IsMessage(err); m != nil {
		return m
	}

	// Sanity check if the error is a gRPC status
	st, ok := status.FromError(err)
	if !ok {
		return New(CommonUnknownError).WithCause(err)
	}

	// Extract the gRPC info from the gRPC status
	grpcInfo := GRPCInfo{
		GRPCCode:    st.Code(),
		GRPCMessage: st.Message(),
	}

	// Initialize variables to hold extracted details
	var (
		domain   string
		reason   string
		locale   string
		metadata map[string]string
		chain    *apapproto.ErrorChain
	)

	// Extract the error info and locale from the gRPC details
	for _, detail := range st.Details() {
		switch d := detail.(type) {
		case *errdetails.ErrorInfo:
			domain = d.Domain
			reason = d.Reason
			metadata = d.Metadata
		case *errdetails.LocalizedMessage:
			if d.Locale != "" {
				locale = d.Locale
			}
		case *anypb.Any:
			if d.MessageIs(&apapproto.ErrorChain{}) {
				ec := &apapproto.ErrorChain{}
				if err := d.UnmarshalTo(ec); err == nil && ec.GetRoot() != nil {
					chain = ec
				}
			}
		}
	}
	// If there are no details, this is a plain error; return string error
	if len(st.Details()) == 0 {
		return errors.New(st.Message())
	}
	// This isn't a plain error, but also isn't a valid message; throw new unknown error
	if domain == "" || reason == "" {
		return New(CommonUnknownError).WithCause(err)
	}

	// If we have a chain, fully reconstruct it and return.
	if chain != nil {
		rebuilt := ReconstructFromChain(chain)
		// Attach gRPC info to topmost MessageImpl if applicable
		if top, ok := rebuilt.(*MessageImpl); ok {
			top.grpcInfo = grpcInfo
		}
		return rebuilt
	}

	// Fallback: Construct a flat top-level Message from ErrorInfo only
	msg := &MessageImpl{
		domain:   domain,
		reason:   reason,
		metadata: metadata,
		locale:   locale,
		grpcInfo: grpcInfo,
	}

	return msg
}

// Interface that unwraps one error, one level only (i.e. do not traverse)
type unwrapOne interface {
	Unwrap() error
}

// Interface that unwraps many errors, one-level only (i.e. do not traverse)
type unwrapMany interface {
	Unwrap() []error
}

// childErrors is a helper function that extracts the direct children of an error.
// It checks if the error implements unwrapMany or unwrapOne interfaces to get
// the children without traversing the entire error chain. Returns either
// []{Unwrap(err)} or Join children, or nil.
func childErrors(err error) []error {
	if err == nil {
		return nil
	}
	// Multi-cause first
	if u, ok := err.(unwrapMany); ok && u.Unwrap() != nil {
		return u.Unwrap()
	}
	// Single-cause
	if u, ok := err.(unwrapOne); ok && u.Unwrap() != nil {
		return []error{u.Unwrap()}
	}
	return nil
}

// toNode converts the current error (single node) into a proto node,
// and recursively converts its children.
func toNode(err error, seen map[error]struct{}) *apapproto.ErrorNode {
	if err == nil {
		return nil
	}
	if _, ok := seen[err]; ok {
		return &apapproto.ErrorNode{
			Error: err.Error(),
			Type:  fmt.Sprintf("%T", err),
		}
	}
	seen[err] = struct{}{}

	n := &apapproto.ErrorNode{
		Error: err.Error(),
		Type:  fmt.Sprintf("%T", err),
	}

	// Using a direct type assertion here so that we only tag the current node
	// and not its children.
	if m, ok := err.(*MessageImpl); ok {
		n.Message = &apapproto.MessageDetails{
			Code:     m.Code(),
			Metadata: m.Metadata(),
			Locale:   m.Locale(),
		}
	}
	for _, c := range childErrors(err) {
		n.Children = append(n.Children, toNode(c, seen))
	}
	return n
}

// BuildErrorChain builds a proto ErrorChain from the given root error.
func BuildErrorChain(root error) *apapproto.ErrorChain {
	if root == nil {
		return nil
	}
	return &apapproto.ErrorChain{Root: toNode(root, make(map[error]struct{}))}
}

// fromNode reconstructs an error from a proto node (recursive).
func fromNode(n *apapproto.ErrorNode) error {
	if n == nil {
		return nil
	}
	// Rebuild children first
	children := make([]error, 0, len(n.Children))
	for _, c := range n.Children {
		if c != nil {
			children = append(children, fromNode(c))
		}
	}

	// MessageImpl node
	if md := n.Message; md != nil && md.Code != "" {
		msg := New(MessageCode(md.Code))
		if msg == nil {
			// Fall back to plain if code malformed
			return plainFrom(n.Error, children)
		}
		msg.locale = md.Locale
		msg = msg.WithMetadata(md.Metadata)
		switch len(children) {
		case 0:
			return msg
		case 1:
			return msg.WithCause(children[0])
		default:
			return msg.WithCause(errors.Join(children...))
		}
	}

	// If we reach here, return a plain error
	return plainFrom(n.Error, children)
}

// ReconstructFromChain reconstructs an error from a proto ErrorChain.
func ReconstructFromChain(chain *apapproto.ErrorChain) error {
	if chain == nil {
		return nil
	}
	return fromNode(chain.GetRoot())
}

// plainErr is a helper function that builds a plain node.
func plainFrom(msg string, children []error) error {
	switch len(children) {
	case 0:
		return errors.New(msg)
	case 1:
		return fmt.Errorf("%s: %w", msg, children[0])
	default:
		return fmt.Errorf("%s: %w", msg, errors.Join(children...))
	}
}

// Compile time safety checks to ensure MessageImpl implements the Message interface
var _ Message = (*MessageImpl)(nil)
var _ error = (*MessageImpl)(nil)
