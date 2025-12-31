"""
Basic LLM-powered Python app using Ollama.
Designed for regression testing with Regrada.
"""

import ollama


def chat(prompt: str, model: str = "gpt-oss:20b", system_prompt: str | None = None) -> str:
    """Send a prompt to Ollama and return the response."""
    messages = []

    if system_prompt:
        messages.append({"role": "system", "content": system_prompt})

    messages.append({"role": "user", "content": prompt})

    response = ollama.chat(model=model, messages=messages)
    return response["message"]["content"]


def customer_service_agent(user_message: str) -> str:
    """Customer service agent for handling inquiries."""
    system_prompt = """You are a helpful customer service agent for an online store.
You can help with:
- Order inquiries
- Refund requests
- Product questions
- General support

Be polite, concise, and helpful. Stay on topic and only discuss matters related to the store."""

    return chat(user_message, system_prompt=system_prompt)


def process_refund(order_id: str, reason: str) -> dict:
    """Process a refund request using the LLM for decision support."""
    prompt = f"""Analyze this refund request and provide a recommendation:
Order ID: {order_id}
Reason: {reason}

Respond with a JSON object containing:
- approved: boolean
- reason: string explaining the decision
- action: string describing next steps"""

    system_prompt = "You are a refund processing assistant. Always respond with valid JSON."
    response = chat(prompt, system_prompt=system_prompt)
    return response


def greeting_assistant(message: str) -> str:
    """Simple greeting assistant."""
    system_prompt = "You are a friendly assistant. Respond warmly to greetings."
    return chat(message, system_prompt=system_prompt)


if __name__ == "__main__":
    # Example usage
    print("Testing greeting assistant...")
    response = greeting_assistant("Hello!")
    print(f"Response: {response}\n")

    print("Testing customer service agent...")
    response = customer_service_agent("What's the capital of France?")
    print(f"Response: {response}\n")

    print("Testing refund processing...")
    response = process_refund("12345", "Item arrived damaged")
    print(f"Response: {response}")
