# Use the official Golang image so the 'go' command is available at runtime
FROM golang:1.22-alpine

# Set the working directory inside the container
WORKDIR /app

# Copy your Go server code into the container
COPY main.go go.mod ./

# Build your API server
RUN go build -o server main.go

# Add a non-root user for slight security improvement
RUN adduser -D gopher
USER gopher

# Expose the port Render expects
EXPOSE 10000

# Run the compiled server
CMD ["./server"]