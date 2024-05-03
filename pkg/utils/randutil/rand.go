package randutil

import (
	"math/rand"
	"time"
)

// 常用字母表
const (
	AlphaLower = "abcdefghijklmnopqrstuvwxyz"
	Numeric    = "0123456789"
)

// Rand 随机数生成器
type Rand struct {
	*rand.Rand
}

// NewRand 创建带有默认 rand.Source 的随机数生成器
func NewRand() *Rand {
	return NewRandWithSource(rand.NewSource(time.Now().UnixNano()))
}

// NewRandWithSource 使用指定的 rand.Source 创建随机数生成器
func NewRandWithSource(source rand.Source) *Rand {
	return &Rand{Rand: rand.New(source)}
}

// CharN 返回包含 letters 中随机 n 个字符的字符串
func (r *Rand) CharN(letters string, n int) string {
	lettersSlice := []byte(letters)
	randSlice := make([]byte, n)

	for i := range randSlice {
		randSlice[i] = lettersSlice[r.Intn(len(lettersSlice))]
	}

	return string(randSlice)
}

// LowerAlphaNumN 返回包含 n 个随机小写字母和数字的字符串
func (r *Rand) LowerAlphaNumN(n int) string {
	return r.CharN(AlphaLower+Numeric, n)
}
