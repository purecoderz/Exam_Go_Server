# Start from the official Go image
FROM golang:alpine

# Install git (needed for go get)
RUN apk add --no-cache git

# Set the working directory
WORKDIR /app

# 1. THE FIX: Handle your server's own dependencies
COPY go.mod go.sum ./ 
RUN go mod download

# 2. THE FIX: Pre-install the z01 library for students
# We create a dummy module to "warm up" the cache with z01
RUN mkdir -p /student_env && cd /student_env && \
    go mod init student_env && \
    go get github.com/01-edu/z01 && \
    go mod download

# Now copy the rest of your server code
COPY . .

# Build the application
RUN go build -o server main.go

# Expose the port
EXPOSE 3001

# Run the executable
CMD ["./server"]