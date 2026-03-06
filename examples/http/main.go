// Example: HTTP server with SSE and Vercel AI SDK protocol handlers.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config"
	"github.com/sadhakbj/aisdk-go/transport"
)

// ChatAgent is a simple chat agent for the HTTP example.
type ChatAgent struct {
	aisdk.BaseAgent
}

func NewChatAgent() *ChatAgent {
	return &ChatAgent{
		BaseAgent: aisdk.NewBaseAgent(aisdk.AgentConfig{
			Model:        "fast",
			Instructions: "You are a helpful assistant. Be concise and friendly.",
			MaxSteps:     1,
			Timeout:      30 * time.Second,
		}),
	}
}

func main() {
	agent := NewChatAgent()
	mux := http.NewServeMux()

	// Standard SSE endpoint
	// POST /api/chat/sse with {"prompt": "Hello!"}
	mux.Handle("/api/chat/sse", transport.SSEHandler(agent))

	// Vercel AI SDK protocol endpoint
	// POST /api/chat with {"messages": [{"role": "user", "content": "Hello!"}]}
	// Works with Next.js useChat() hook out of the box.
	mux.Handle("/api/chat", transport.VercelHandler(agent))

	// Simple health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status": "ok"}`)
	})

	// Serve a simple HTML page for testing SSE
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, indexHTML)
	})

	addr := ":8081"
	log.Printf("Server starting on %s", addr)
	log.Printf("  SSE endpoint:    POST %s/api/chat/sse", addr)
	log.Printf("  Vercel endpoint: POST %s/api/chat", addr)
	log.Printf("  Test UI:         GET  %s/", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

const indexHTML = `<!DOCTYPE html>
<html>
<head>
    <title>aisdk-go Chat</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 600px; margin: 40px auto; padding: 20px; }
        #messages { border: 1px solid #ddd; padding: 16px; min-height: 200px; border-radius: 8px; margin-bottom: 16px; }
        .msg { margin: 8px 0; }
        .user { color: #0066cc; }
        .assistant { color: #333; }
        input { width: 70%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        button { padding: 8px 16px; background: #0066cc; color: white; border: none; border-radius: 4px; cursor: pointer; }
    </style>
</head>
<body>
    <h1>aisdk-go Chat</h1>
    <div id="messages"></div>
    <form onsubmit="send(event)">
        <input id="input" placeholder="Type a message..." autofocus />
        <button type="submit">Send</button>
    </form>
    <script>
        async function send(e) {
            e.preventDefault();
            const input = document.getElementById('input');
            const msg = input.value.trim();
            if (!msg) return;
            input.value = '';

            const messages = document.getElementById('messages');
            messages.innerHTML += '<div class="msg user"><b>You:</b> ' + msg + '</div>';

            const resp = await fetch('/api/chat/sse', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ prompt: msg })
            });

            const reader = resp.body.getReader();
            const decoder = new TextDecoder();
            let assistantDiv = document.createElement('div');
            assistantDiv.className = 'msg assistant';
            assistantDiv.innerHTML = '<b>AI:</b> ';
            messages.appendChild(assistantDiv);
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop();
                for (const line of lines) {
                    if (line.startsWith('data: ') && line !== 'data: [DONE]') {
                        try {
                            const event = JSON.parse(line.slice(6));
                            if (event.type === 'text') {
                                assistantDiv.innerHTML += event.text;
                            } else if (event.type === 'error') {
                                assistantDiv.innerHTML += '<em>[Error: ' + event.error + ']</em>';
                            }
                        } catch {}
                    }
                }
            }
        }
    </script>
</body>
</html>`
