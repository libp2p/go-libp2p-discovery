package discovery

import (
	"math"
	"math/rand"
	"time"
)

type BackoffFactory func() BackoffStrategy

type BackoffStrategy interface {
	Delay() time.Duration
	Reset()
}

// Jitter implementations taken roughly from https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/

// Jitter must return a duration between min and max. Min must be lower than, or equal to, max.
type Jitter func(duration time.Duration, min time.Duration, max time.Duration, rng *rand.Rand) time.Duration

func FullJitter(duration time.Duration, min time.Duration, max time.Duration, rng *rand.Rand) time.Duration {
	if duration <= min {
		return min
	}

	normalizedDur := boundedDuration(duration, min, max) - min

	return boundedDuration(time.Duration(rng.Int63n(int64(normalizedDur)))+min, min, max)
}

func NoJitter(duration time.Duration, min time.Duration, max time.Duration, rng *rand.Rand) time.Duration {
	return boundedDuration(duration, min, max)
}

type randomizedBackoff struct {
	min time.Duration
	max time.Duration
	rng *rand.Rand
}

func (b *randomizedBackoff) BoundedDelay(duration time.Duration) time.Duration {
	return boundedDuration(duration, b.min, b.max)
}

func boundedDuration(d time.Duration, min time.Duration, max time.Duration) time.Duration {
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}

type attemptBackoff struct {
	attempt int
	jitter  Jitter
	randomizedBackoff
}

func (b *attemptBackoff) Reset() {
	b.attempt = 0
}

func NewFixedBackoff(delay time.Duration) BackoffFactory {
	return func() BackoffStrategy {
		return &fixedBackoff{delay: delay}
	}
}

type fixedBackoff struct {
	delay time.Duration
}

func (b *fixedBackoff) Delay() time.Duration {
	return b.delay
}

func (b *fixedBackoff) Reset() {}

func NewPolynomialBackoff(min, max time.Duration, jitter Jitter,
	timeUnits time.Duration, polyCoefs []float64, rng *rand.Rand) BackoffFactory {
	return func() BackoffStrategy {
		return &polynomialBackoff{
			attemptBackoff: attemptBackoff{
				randomizedBackoff: randomizedBackoff{
					min: min,
					max: max,
					rng: rng,
				},
				jitter: jitter,
			},
			timeUnits: timeUnits,
			poly:      polyCoefs,
		}
	}
}

type polynomialBackoff struct {
	attemptBackoff
	timeUnits time.Duration
	poly      []float64
}

func (b *polynomialBackoff) Delay() time.Duration {
	var polySum float64
	switch len(b.poly) {
	case 0:
		return 0
	case 1:
		polySum = b.poly[0]
	default:
		polySum = b.poly[0]
		exp := 1
		attempt := b.attempt
		b.attempt++

		for _, c := range b.poly[1:] {
			exp *= attempt
			polySum += float64(exp) * c
		}
	}
	return b.jitter(time.Duration(float64(b.timeUnits)*polySum), b.min, b.max, b.rng)
}

func NewExponentialBackoff(min, max time.Duration, jitter Jitter,
	timeUnits time.Duration, base float64, offset time.Duration, rng *rand.Rand) BackoffFactory {
	return func() BackoffStrategy {
		return &exponentialBackoff{
			attemptBackoff: attemptBackoff{
				randomizedBackoff: randomizedBackoff{
					min: min,
					max: max,
					rng: rng,
				},
				jitter: jitter,
			},
			timeUnits: timeUnits,
			base:      base,
			offset:    offset,
		}
	}
}

type exponentialBackoff struct {
	attemptBackoff
	timeUnits time.Duration
	base      float64
	offset    time.Duration
}

func (b *exponentialBackoff) Delay() time.Duration {
	attempt := b.attempt
	b.attempt++
	return b.jitter(
		time.Duration(math.Pow(b.base, float64(attempt))*float64(b.timeUnits))+b.offset, b.min, b.max, b.rng)
}

func NewExponentialDecorrelatedJitter(min, max time.Duration, base float64, rng *rand.Rand) BackoffFactory {
	return func() BackoffStrategy {
		return &exponentialDecorrelatedJitter{
			randomizedBackoff: randomizedBackoff{
				min: min,
				max: max,
				rng: rng,
			},
			base: base,
		}
	}
}

type exponentialDecorrelatedJitter struct {
	randomizedBackoff
	base      float64
	lastDelay time.Duration
}

func (b *exponentialDecorrelatedJitter) Delay() time.Duration {
	if b.lastDelay < b.min {
		b.lastDelay = b.min
		return b.lastDelay
	}

	nextMax := int64(float64(b.lastDelay) * b.base)
	b.lastDelay = boundedDuration(time.Duration(b.rng.Int63n(nextMax-int64(b.min)))+b.min, b.min, b.max)
	return b.lastDelay
}

func (b *exponentialDecorrelatedJitter) Reset() { b.lastDelay = 0 }
