# Records docs/demo.gif: a real transfer between two tmux panes.
#
#   vhs docs/demo.tape
#
# Needs vhs, tmux and tubo in PATH. The relay is TUBO_SERVER (default: the
# public one) and must run the same version as the tubo binary.

Output docs/demo.gif
Require tmux
Require tubo

Set Shell "bash"
Set FontSize 15
Set Width 1500
Set Height 560
Set Padding 16
Set TypingSpeed 35ms
Set Theme "Catppuccin Mocha"

Hide
Type `export BASH_SILENCE_DEPRECATION_WARNING=1; cd "$(mktemp -d)" && mkdir a b && head -c 4000000 /dev/urandom > a/backup.sql`
Enter
Type `tmux -f /dev/null new -s tubo-demo -c "$PWD/a" 'bash --noprofile --norc'`
Enter
Sleep 1s
Type `tmux set status off \; split-window -h -c "$PWD/../b" 'bash --noprofile --norc'`
Enter
Sleep 500ms
Type `export PS1='machine-b $ ' && clear`
Enter
Ctrl+B
Left
Type `export PS1='machine-a $ ' && clear`
Enter
Sleep 500ms
Show

Type "tubo send backup.sql"
Sleep 500ms
Enter
Sleep 3s

# Copy the curl line from the left pane, as a person would
Ctrl+B
Right
Hide
Type `tmux capture-pane -p -J -t tubo-demo:0.0 | grep -m1 'curl -sL' | sed 's/^ *//' | tr -d '\n' | tmux load-buffer - && clear`
Enter
Sleep 500ms
Show
Sleep 800ms
Ctrl+B
Type "]"
Sleep 1.5s
Enter
Sleep 6s

Hide
Type "tmux kill-session -t tubo-demo"
Enter
