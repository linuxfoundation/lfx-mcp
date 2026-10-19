// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

// The recording and transcript index documents of a past meeting carry the
// recording provider's bearer links (download_url, play_url, share_url), which
// no tool returns: content is fetched at the source, never through a provider
// link. get_past_meeting therefore returns a projection of each document, not
// the document: the types below name every field that leaves, and the
// project* functions read exactly those fields from the document's data, so a
// field the index adds tomorrow does not leak. Field names are those of the
// meeting service's docs/indexer-contract.md (Recording document, Recording
// File schema, Recording Session schema, Transcript document).

// pastMeetingRecordingDetails is the file-level view of a past meeting's
// v1_past_meeting_recording document. recording_count counts the source files
// before the index split them between the recording and transcript documents,
// so it can exceed len(files); total_size covers every source file too.
type pastMeetingRecordingDetails struct {
	RecordingAccess string                       `json:"recording_access"`
	StartTime       string                       `json:"start_time,omitempty"`
	RecordingCount  int                          `json:"recording_count"`
	TotalSize       int64                        `json:"total_size"`
	Files           []pastMeetingArtifactFile    `json:"files"`
	Sessions        []pastMeetingArtifactSession `json:"sessions"`
}

// pastMeetingTranscriptDetails is the file-level view of a past meeting's
// v1_past_meeting_transcript document (its TRANSCRIPT and TIMELINE files).
// total_size covers every source file of the recording, not only these.
type pastMeetingTranscriptDetails struct {
	TranscriptAccess string                       `json:"transcript_access,omitempty"`
	StartTime        string                       `json:"start_time,omitempty"`
	TotalSize        int64                        `json:"total_size"`
	Files            []pastMeetingArtifactFile    `json:"files"`
	Sessions         []pastMeetingArtifactSession `json:"sessions"`
}

// pastMeetingArtifactFile is one entry of a document's recording_files: the
// file's type, extension, size, time span, recording type and status. Its id,
// meeting_id, download_url and play_url are not carried.
type pastMeetingArtifactFile struct {
	FileType       string `json:"file_type"`
	FileExtension  string `json:"file_extension"`
	FileSize       int64  `json:"file_size"`
	RecordingStart string `json:"recording_start,omitempty"`
	RecordingEnd   string `json:"recording_end,omitempty"`
	RecordingType  string `json:"recording_type,omitempty"`
	Status         string `json:"status,omitempty"`
}

// pastMeetingArtifactSession is one entry of a document's sessions: when it
// started and its size. Its uuid and share_url are not carried.
type pastMeetingArtifactSession struct {
	StartTime string `json:"start_time,omitempty"`
	TotalSize int64  `json:"total_size"`
}

// projectPastMeetingRecording builds the recording details from a
// v1_past_meeting_recording document's data, reading only the fields the
// result type names. data that is not a JSON object gives empty details.
func projectPastMeetingRecording(data any) *pastMeetingRecordingDetails {
	doc, _ := data.(map[string]any)
	return &pastMeetingRecordingDetails{
		RecordingAccess: stringField(doc, "recording_access"),
		StartTime:       stringField(doc, "start_time"),
		RecordingCount:  int(intField(doc, "recording_count")),
		TotalSize:       intField(doc, "total_size"),
		Files:           projectPastMeetingArtifactFiles(doc["recording_files"]),
		Sessions:        projectPastMeetingArtifactSessions(doc["sessions"]),
	}
}

// projectPastMeetingTranscript builds the transcript details from a
// v1_past_meeting_transcript document's data, reading only the fields the
// result type names. data that is not a JSON object gives empty details.
func projectPastMeetingTranscript(data any) *pastMeetingTranscriptDetails {
	doc, _ := data.(map[string]any)
	return &pastMeetingTranscriptDetails{
		TranscriptAccess: stringField(doc, "transcript_access"),
		StartTime:        stringField(doc, "start_time"),
		TotalSize:        intField(doc, "total_size"),
		Files:            projectPastMeetingArtifactFiles(doc["recording_files"]),
		Sessions:         projectPastMeetingArtifactSessions(doc["sessions"]),
	}
}

// projectPastMeetingArtifactFiles projects a document's recording_files. The
// result is an empty slice, never nil, when the field is absent, null or not
// an array; entries that are not objects are skipped.
func projectPastMeetingArtifactFiles(v any) []pastMeetingArtifactFile {
	entries, _ := v.([]any)
	files := make([]pastMeetingArtifactFile, 0, len(entries))
	for _, e := range entries {
		f, ok := e.(map[string]any)
		if !ok {
			continue
		}
		files = append(files, pastMeetingArtifactFile{
			FileType:       stringField(f, "file_type"),
			FileExtension:  stringField(f, "file_extension"),
			FileSize:       intField(f, "file_size"),
			RecordingStart: stringField(f, "recording_start"),
			RecordingEnd:   stringField(f, "recording_end"),
			RecordingType:  stringField(f, "recording_type"),
			Status:         stringField(f, "status"),
		})
	}
	return files
}

// projectPastMeetingArtifactSessions projects a document's sessions, with the
// same empty-not-nil rule as projectPastMeetingArtifactFiles.
func projectPastMeetingArtifactSessions(v any) []pastMeetingArtifactSession {
	entries, _ := v.([]any)
	sessions := make([]pastMeetingArtifactSession, 0, len(entries))
	for _, e := range entries {
		s, ok := e.(map[string]any)
		if !ok {
			continue
		}
		sessions = append(sessions, pastMeetingArtifactSession{
			StartTime: stringField(s, "start_time"),
			TotalSize: intField(s, "total_size"),
		})
	}
	return sessions
}

// stringField returns doc[key] when it is a string, else "".
func stringField(doc map[string]any, key string) string {
	s, _ := doc[key].(string)
	return s
}

// intField returns doc[key] as an int64 when it is a JSON number (decoded as
// float64) or an integer, else 0. A missing file_size is 0.
func intField(doc map[string]any, key string) int64 {
	switch n := doc[key].(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
