"""
Basic LLM-powered Python app using Ollama.
Designed for regression testing with Regrada.
"""

import json
import ollama


# Tool definitions
TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "get_weather",
            "description": "Get the weather for a given city",
            "parameters": {
                "type": "object",
                "properties": {
                    "city": {
                        "type": "string",
                        "description": "The city to get the weather for",
                    },
                },
                "required": ["city"],
            },
        }
    },
    {
        "type": "function",
        "function": {
            "name": "process_refund",
            "description": "Process a refund for a customer order",
            "parameters": {
                "type": "object",
                "properties": {
                    "order_id": {
                        "type": "string",
                        "description": "The order ID to refund",
                    },
                    "reason": {
                        "type": "string",
                        "description": "Reason for the refund",
                    },
                },
                "required": ["order_id", "reason"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "create_purchase",
            "description": "Create a new purchase order for a customer",
            "parameters": {
                "type": "object",
                "properties": {
                    "product_id": {
                        "type": "string",
                        "description": "The product ID to purchase",
                    },
                    "quantity": {
                        "type": "integer",
                        "description": "Quantity to purchase",
                    },
                },
                "required": ["product_id", "quantity"],
            },
        },
    },
]


def chat(prompt: str, model: str = "qwen3:4b", system_prompt: str | None = None, use_tools: bool = False) -> dict:
    """Send a prompt to Ollama and return the response."""
    messages = []

    if system_prompt:
        messages.append({"role": "system", "content": system_prompt})

    messages.append({"role": "user", "content": prompt})

    kwargs = {"model": model, "messages": messages}
    if use_tools:
        kwargs["tools"] = TOOLS

    response = ollama.chat(**kwargs)
    return response


def customer_service_agent(user_message: str) -> dict:
    """Customer service agent for handling inquiries with tool support."""
    system_prompt = """You are a helpful customer service agent for an online store.
You can help with:
- Order inquiries
- Refund requests
- Product questions
- General support

Be polite, concise, and helpful. Do not answer questions that are not related to the store.
Use the available tools when appropriate."""

    return chat(user_message, system_prompt=system_prompt, use_tools=True)


def refund_handler(user_message: str) -> dict:
    """Handle refund requests using tools."""
    system_prompt = """You are a refund processing assistant.
When a customer requests a refund, use the process_refund tool to handle it."""
    return chat(user_message, system_prompt=system_prompt, use_tools=True)


def purchase_handler(user_message: str) -> dict:
    """Handle purchase requests using tools."""
    system_prompt = """You are a purchase assistant.
When a customer wants to buy something, use the create_purchase tool to handle it."""
    return chat(user_message, system_prompt=system_prompt, use_tools=True)


def greeting_assistant(message: str) -> dict:
    """Simple greeting assistant."""
    system_prompt = "You are a friendly assistant. Respond warmly to greetings."
    return chat(message, system_prompt=system_prompt)


def weather_assistant(message: str) -> dict:
    """Weather assistant for getting the weather in a given city."""
    system_prompt = "You are a weather assistant. You can get the weather for a given city."
    return chat(message, system_prompt=system_prompt, use_tools=True)

if __name__ == "__main__":
    # Example usage
    print("Testing greeting assistant...")
    response = greeting_assistant("Hello!")
    print(f"Response: {response}\n")

    print("Testing greeting assistant 2...")
    response = greeting_assistant("What is 2+2? Just give me the number. No emojis, no fluff. Just the number.")
    print(f"Response: {response}\n")

    print("Testing greeting assistant 3...")
    response = greeting_assistant("Hi there!")
    print(f"Response: {response}\n")

    print("Testing weather assistant...")
    response = weather_assistant("What's the weather in Tokyo?")
    print(f"Response: {response}\n")

    # print("Testing customer service agent...")
    # response = customer_service_agent("What's the capital of France?")
    # print(f"Response: {response}\n")

    # print("Testing refund handler...")
    # response = refund_handler("I want to return order #12345, it arrived damaged")
    # print(f"Response: {response}\n")

    # print("Testing purchase handler...")
    # response = purchase_handler("I'd like to buy product ABC123, quantity 2")
    # print(f"Response: {response}")
