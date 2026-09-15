import re, sys, statistics, collections
# usage: agg.py out.tsv bench1.txt [bench2.txt ...] -> name \t median ns \t min \t max \t runs \t allocs
rows = collections.defaultdict(list)
allocs = {}
pat = re.compile(r'^(Benchmark\S+?)(?:-\d+)?\s+\d+\s+([\d.]+) ns/op(?:\s+([\d.]+) B/op\s+([\d.]+) allocs/op)?')
for f in sys.argv[2:]:
    for line in open(f, encoding='utf-8', errors='replace'):
        mm = pat.match(line.strip())
        if mm:
            rows[mm.group(1)].append(float(mm.group(2)))
            if mm.group(4) is not None:
                allocs[mm.group(1)] = max(allocs.get(mm.group(1), 0), float(mm.group(4)))
with open(sys.argv[1], 'w', encoding='utf-8', newline='\n') as o:
    o.write('name\tmedian\tmin\tmax\truns\tallocs\n')
    for k, v in rows.items():
        o.write(f'{k}\t{statistics.median(v):.1f}\t{min(v):.1f}\t{max(v):.1f}\t{len(v)}\t{allocs.get(k, "")}\n')
print(len(rows), 'benchmarks')
