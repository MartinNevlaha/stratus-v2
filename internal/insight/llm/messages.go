package llm

func SystemPrompt(content string) Message {
	return Message{Role: "system", Content: content}
}

func UserMessage(content string) Message {
	return Message{Role: "user", Content: content}
}

func AssistantMessage(content string) Message {
	return Message{Role: "assistant", Content: content}
}

func BuildMessages(systemPrompt string, userMessages ...string) []Message {
	msgs := make([]Message, 0, len(userMessages)+1)
	if systemPrompt != "" {
		msgs = append(msgs, SystemPrompt(systemPrompt))
	}
	for _, content := range userMessages {
		msgs = append(msgs, UserMessage(content))
	}
	return msgs
}

func BuildConversation(systemPrompt string, turns ...Message) []Message {
	msgs := make([]Message, 0, len(turns)+1)
	if systemPrompt != "" {
		msgs = append(msgs, SystemPrompt(systemPrompt))
	}
	msgs = append(msgs, turns...)
	return msgs
}
