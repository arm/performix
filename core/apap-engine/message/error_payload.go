// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

type ErrorPayload struct {
	Code        MessageCode       `json:"message_code"`
	Severity    string            `json:"severity"`
	Message     string            `json:"message"`
	Explanation string            `json:"explanation"`
	Advice      string            `json:"advice"`
	Locale      string            `json:"locale"`
	Metadata    map[string]string `json:"metadata"`
	Children    []*ErrorPayload   `json:"children,omitempty"`
}

type ErrorPayloadOptions struct {
	LookupMessage    func(err error) (*CatalogMessage, error)
	FormatNonMessage func(err error) string
}

func DirectChildren(err error) []error {
	if err == nil {
		return nil
	}

	if u, ok := err.(unwrapMany); ok {
		if children := u.Unwrap(); len(children) > 0 {
			return children
		}
	}

	if u, ok := err.(unwrapOne); ok {
		if child := u.Unwrap(); child != nil {
			return []error{child}
		}
	}

	return nil
}

// BuildErrorPayload builds a full ErrorPayload tree for JSON output.
// It recurses into all wrapped errors, looking up each node in the catalog
// if it is a MessageImpl. If the node is not a MessageImpl, it builds a plain
// error node with just the raw error string.
func BuildErrorPayload(err error, opts *ErrorPayloadOptions) *ErrorPayload {
	if err == nil {
		return nil
	}

	if msg, ok := err.(*MessageImpl); ok && msg != nil {
		payload := &ErrorPayload{
			Code:     msg.Code(),
			Locale:   msg.Locale(),
			Metadata: msg.Metadata(),
		}

		lookup := LookupMessage
		if opts != nil && opts.LookupMessage != nil {
			lookup = opts.LookupMessage
		}

		if catalogMsg, lookupErr := lookup(msg); lookupErr == nil {
			payload.Severity = catalogMsg.Severity
			payload.Message = catalogMsg.Message
			payload.Explanation = catalogMsg.Explanation
			payload.Advice = catalogMsg.Advice
		} else {
			payload.Severity = string(SeverityError)
			payload.Message = msg.Error()
		}

		for _, child := range DirectChildren(err) {
			if node := BuildErrorPayload(child, opts); node != nil {
				payload.Children = append(payload.Children, node)
			}
		}

		return payload
	}

	messageText := err.Error()
	if opts != nil && opts.FormatNonMessage != nil {
		messageText = opts.FormatNonMessage(err)
	}

	payload := &ErrorPayload{
		Severity: string(SeverityError),
		Message:  messageText,
	}

	for _, child := range DirectChildren(err) {
		if node := BuildErrorPayload(child, opts); node != nil {
			payload.Children = append(payload.Children, node)
		}
	}

	return payload
}
