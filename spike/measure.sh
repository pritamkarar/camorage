# M0: per-process CPU (% of one core) and RSS over a window.  usage: bash measure.sh <seconds> <pid>...
T=$1; shift
TICK=100   # CLK_TCK on Linux/Android ARM
declare -A before
for p in "$@"; do before[$p]=$(awk '{print $14+$15}' /proc/$p/stat); done
sleep "$T"
tot=0; rss=0
for p in "$@"; do
  if [ ! -r /proc/$p/stat ]; then echo "$p DIED"; continue; fi
  now=$(awk '{print $14+$15}' /proc/$p/stat)
  cpu=$(awk -v d=$((now - before[$p])) -v t="$T" -v k=$TICK 'BEGIN{printf "%.1f", d/k/t*100}')
  r=$(awk '/VmRSS/{print int($2/1024)}' /proc/$p/status)
  echo "$p $(cat /proc/$p/comm) cpu=${cpu}% rss=${r}MB"
  tot=$(awk -v x="$tot" -v y="$cpu" 'BEGIN{print x+y}'); rss=$((rss + r))
done
echo "TOTAL cpu=${tot}% of one core ($(nproc) cores) rss=${rss}MB"
