package ga

import (
	"math/rand"
	"runtime"
	"sort"
	"sync"

	"github.com/pennyee12/Quant/internal/genome"
)

// Config controls the GA evolution loop.
type Config struct {
	PopSize           int
	MaxGenerations    int
	TournamentSize    int
	Elitism           int     // number of top individuals carried unchanged
	MutationProb      float64 // initial per-gene mutation probability
	MutationScale     float64 // initial mutation amplitude scale
	RampFactor        float64 // multiplier when no improvement (default 1.25)
	EarlyStopPatience int     // generations without improvement before ramping
	EarlyStopDelta    float64 // minimum improvement to reset patience counter
	ProbMax           float64 // mutation probability ceiling
	ScaleMax          float64 // mutation scale ceiling
	Workers           int     // parallel fitness evaluators (0 = NumCPU)
}

func DefaultConfig() Config {
	return Config{
		PopSize:           100,
		MaxGenerations:    30,
		TournamentSize:    3,
		Elitism:           5,
		MutationProb:      0.15,
		MutationScale:     1.0,
		RampFactor:        1.25,
		EarlyStopPatience: 5,
		EarlyStopDelta:    0.001,
		ProbMax:           0.55,
		ScaleMax:          3.0,
		Workers:           0,
	}
}

// EvalFunc scores a chromosome. Higher is better. Return FatalScore on disqualification.
type EvalFunc func(c genome.Chromosome) float64

// Run executes the GA and returns the best chromosome found.
// seed is the default starting individual (index 0 of the initial population).
// elites are extra chromosomes to seed the initial population (from DB champions).
// progress is called after each generation with (gen, bestScore); may be nil.
func Run(
	eval EvalFunc,
	seed genome.Chromosome,
	elites []genome.Chromosome,
	cfg Config,
	rng *rand.Rand,
	progress func(gen int, best float64),
) genome.Chromosome {
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	pop := initPop(seed, elites, cfg.PopSize, rng)
	scores := evalParallel(pop, eval, workers)

	bestScore := best(scores)
	noImprove := 0
	mutProb := cfg.MutationProb
	mutScale := cfg.MutationScale

	for gen := 0; gen < cfg.MaxGenerations; gen++ {
		if progress != nil {
			progress(gen, bestScore)
		}

		genBest := best(scores)
		if genBest-bestScore > cfg.EarlyStopDelta {
			bestScore = genBest
			noImprove = 0
		} else {
			noImprove++
		}

		// Mutation ramp: increase diversity when stuck
		if noImprove >= cfg.EarlyStopPatience {
			mutProb = min(mutProb*cfg.RampFactor, cfg.ProbMax)
			mutScale = min(mutScale*cfg.RampFactor, cfg.ScaleMax)
			// True early stop only once both ceilings are hit and still no improvement
			if mutProb >= cfg.ProbMax && mutScale >= cfg.ScaleMax {
				break
			}
		}

		pop = nextGeneration(pop, scores, cfg, mutProb, mutScale, rng)
		scores = evalParallel(pop, eval, workers)
	}

	return pop[bestIdx(scores)]
}

// ── internal helpers ──────────────────────────────────────────────────────────

func initPop(seed genome.Chromosome, elites []genome.Chromosome, size int, rng *rand.Rand) []genome.Chromosome {
	pop := make([]genome.Chromosome, size)
	pop[0] = seed // index 0 is always the seed champion

	eliteCount := min(len(elites), size/10)
	for i := 0; i < eliteCount && i+1 < size; i++ {
		pop[i+1] = elites[i]
	}

	// 40% mutated from elites (or seed if no elites)
	mutatedCount := size * 40 / 100
	source := seed
	if len(elites) > 0 {
		source = elites[0]
	}
	for i := eliteCount + 1; i <= eliteCount+mutatedCount && i < size; i++ {
		pop[i] = genome.Mutate(source, 0.15, 1.5, rng)
	}

	// Rest: fully random
	for i := eliteCount + mutatedCount + 1; i < size; i++ {
		pop[i] = genome.Sample(rng)
	}

	return pop
}

func nextGeneration(
	pop []genome.Chromosome,
	scores []float64,
	cfg Config,
	mutProb, mutScale float64,
	rng *rand.Rand,
) []genome.Chromosome {
	next := make([]genome.Chromosome, len(pop))

	// Elites carried unchanged
	eliteIdxs := topN(scores, cfg.Elitism)
	for i, idx := range eliteIdxs {
		next[i] = pop[idx]
	}

	// Fill rest with tournament → crossover → mutate
	for i := cfg.Elitism; i < len(pop); i++ {
		p1 := pop[tournament(scores, cfg.TournamentSize, rng)]
		p2 := pop[tournament(scores, cfg.TournamentSize, rng)]
		child := genome.Crossover(p1, p2, rng)
		child = genome.Mutate(child, mutProb, mutScale, rng)
		next[i] = child
	}

	return next
}

func tournament(scores []float64, size int, rng *rand.Rand) int {
	best := rng.Intn(len(scores))
	for i := 1; i < size; i++ {
		idx := rng.Intn(len(scores))
		if scores[idx] > scores[best] {
			best = idx
		}
	}
	return best
}

func topN(scores []float64, n int) []int {
	type pair struct {
		idx   int
		score float64
	}
	pairs := make([]pair, len(scores))
	for i, s := range scores {
		pairs[i] = pair{i, s}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	out := make([]int, min(n, len(pairs)))
	for i := range out {
		out[i] = pairs[i].idx
	}
	return out
}

func evalParallel(pop []genome.Chromosome, eval EvalFunc, workers int) []float64 {
	n := len(pop)
	scores := make([]float64, n)

	type job struct {
		idx int
		c   genome.Chromosome
	}
	jobs := make(chan job, n)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				scores[j.idx] = eval(j.c)
			}
		}()
	}
	for i, c := range pop {
		jobs <- job{i, c}
	}
	close(jobs)
	wg.Wait()
	return scores
}

func best(scores []float64) float64 {
	b := scores[0]
	for _, s := range scores[1:] {
		if s > b {
			b = s
		}
	}
	return b
}

func bestIdx(scores []float64) int {
	idx := 0
	for i, s := range scores[1:] {
		if s > scores[idx] {
			idx = i + 1
		}
	}
	return idx
}
