Output docs/demo.gif
Set FontSize 18
Set Width 1600
Set Height 450
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful

Hide
Type "tmux kill-server"
Enter
Sleep 1s
Type "tmux new -s tubo_demo"
Enter
Type "tmux set -g status off"
Enter
Type "tmux split-window -h"
Enter

# Now in Pane 1 (Right)
Type "export PROMPT='machine-b$ '"
Enter
Type "export PS1='machine-b$ '"
Enter
Type "export PATH=$PWD/docs/bin:$PATH"
Enter
Type "clear"
Enter

# Switch to Pane 0 (Left)
Ctrl+B
Left
Sleep 500ms

# Now in Pane 0 (Left)
Type "export PROMPT='machine-a$ '"
Enter
Type "export PS1='machine-a$ '"
Enter
Type "export PATH=$PWD/docs/bin:$PATH"
Enter
Type "clear"
Enter

# Record!
Show

Type "tubo send secret.sql"
Sleep 500ms
Enter

Sleep 1.5s
Ctrl+B
Right
Sleep 500ms

Type "curl -sL https://tubo.endlessite.com/run | sh -s receive 9xz33a-35qd44o5-BzLnYdWzy4DdoLoI"
Sleep 500ms
Enter

Sleep 5s
