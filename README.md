
# gocbr


gocbr is a library for building Circuit Breakers in Go. It is inspired by [gobreaker](https://github.com/sony/gobreaker) and [hystrix-go](https://github.com/afex/hystrix-go), but it is not a port of either. It is a simple implementation of the [Circuit Breaker Pattern](https://martinfowler.com/bliki/CircuitBreaker.html).

## Features

- Configurable number of requests to track
- Configurable interval to track requests
- Configurable timeout for the circuit breaker
- Callbacks for state changes

## Installation

```bash  
go get -u github.com/ashwineaso/gocbr  
```  

## Usage

```go  
package main  
  
import (  
    "errors"  
    "fmt"
	"time"
  
    "github.com/ashwineaso/gocbr"  
)  
  
func main() {  
    cb := gocbr.NewCircuitBreaker(gocbr.Config{  
        Name:        "my-circuit-breaker",  
        MaxRequests: 10,  
        Interval:    time.Second * 10,  
        Timeout:     time.Second * 60,  
        OnStateChange: func(name string, from gocbr.State, to gocbr.State) {
            fmt.Printf("Circuit breaker %s changed state from %s to %s\n", name, from, to)  
        },  
    })  
  
    // Check if the circuit breaker is open  
    if cb.IsOpen() {
        fmt.Println("Circuit breaker is open")
        return  
    }  
  
    // Execute the function   
    err := func() error {  
        // Do something  
        return errors.New("some error")  
    }()  
    if err != nil {  
        // Report the error to the circuit breaker  
        cb.AddFailure()  
    } else {  
        // Report success to the circuit breaker  
        cb.AddSuccess()  
    }  
}  
```