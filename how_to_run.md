## 1) 
```go run . -port :5001```

## 2) 
```go run . -port :5002 -replicas "localhost:5001"```

## 3) 
```go run . -port :6000 -leader -replicas "localhost:5001, localhost:5002" -duration 20```

## 4) 
```go run . -server localhost:6000 bid <bidder> <bid>```