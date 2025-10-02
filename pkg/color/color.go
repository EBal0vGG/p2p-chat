package color

import (
    "hash/fnv"
)

// Simple deterministic color index (0..n-1)
func IndexFor(id string, n int) int {
    h := fnv.New32a()
    h.Write([]byte(id))
    return int(h.Sum32()) % n
}