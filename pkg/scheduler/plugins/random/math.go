package random

import (
	"math"
	"math/rand"
	"sort"
	"time"
)

// generate a random float64
// if decimal = 2, the result is accurate to two decimal places, such as 1.23/2.34 etc
// if decimal = 0, the result is an integer
// if decimal = -2, the result is an integer multiple of 100, such as 100/1200 etc
func randomFloat64(max float64, decimal int) float64 {
	rand.Seed(time.Now().UnixNano())
	random := rand.Float64()
	random = random * max
	pow := math.Pow10(decimal)
	randomInt64 := int64(random * pow)
	randomFloat64 := float64(randomInt64) / pow
	return randomFloat64
}

// 截绳法, generate num random float64 whose sum is sum
func randomNumFloat64(sum float64, num int, decimal int) []float64 {
	if num <= 0 {
		return nil
	}
	if num == 1 {
		return []float64{randomFloat64(sum, decimal)}
	}
	nodes := make([]float64, 0)
	nodes = append(nodes, 0.0)
	for i := 0; i < num-1; i++ {
		node := randomFloat64(sum, decimal)
		nodes = append(nodes, node)
	}
	nodes = append(nodes, sum)
	sort.Float64s(nodes)
	result := make([]float64, 0)
	for i := 1; i < len(nodes); i++ {
		result = append(result, sub(nodes[i], nodes[i-1], decimal))
	}
	return result
}

func sub(l, r float64, decimal int) float64 {
	pow := math.Pow10(decimal)
	subInt64 := int64(l*pow) - int64(r*pow)
	subFloat64 := float64(subInt64) / pow
	return subFloat64
}
