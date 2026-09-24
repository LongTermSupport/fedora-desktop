### --main


# User specific aliases and functions
alias rm='rm -i'
alias cp='cp -i'
alias mv='mv -i'
alias ll='ls -alh --color'

#real time stamps in dmesg
alias dmesg='dmesg -T'

# Make parent dirs if they are missing
alias mkdir='mkdir -pv'

# Handle UTF-8 with less
export LESSCHARSET=utf-8

# History: every command on disk at the next prompt, timestamped, never truncated.
# The file is not ~/.bash_history, so an interactive shell that neither read this file nor
# inherited the exported sizes (env -i bash, a container sharing $HOME) truncates that
# abandoned default to 500 lines on exit instead of the real history.
# -O: only a directory this user owns — root keeping a user's HOME must not write into it.
# play-basic-configs.yml creates the directory; bash saves nothing if it is missing.
#
# history-search.bash may start a shell with HISTFILE at /dev/null, so that up-arrow holds
# only this terminal's commands, and leave the real file in __history_shared_file. The first
# prompt points HISTFILE back at it, before anything is appended.
shopt -s histappend cmdhist lithist histverify
HISTCONTROL=ignoreboth
export HISTFILESIZE=-1
export HISTSIZE=-1
export HISTIGNORE="ls:[bf]g:exit"
HISTTIMEFORMAT='%F %T  '
if [[ -O "${HOME}/.local/state/bash" ]]; then
    HISTFILE="${HOME}/.local/state/bash/history"
elif [[ $- == *i* ]]; then
    echo "bash history: ${HOME}/.local/state/bash is missing or not owned by $(id -un); history goes to ${HISTFILE:-the default file}. Re-run play-basic-configs.yml." >&2
fi
__history_append() {
    if [[ -n "${__history_shared_file-}" ]]; then
        HISTFILE="${__history_shared_file}"
    fi
    builtin history -a
}
if [[ " ${PROMPT_COMMAND[*]-} " != *" __history_append "* ]]; then
    PROMPT_COMMAND+=(__history_append)
fi

# User local bin
if [[ -d ~/.local/bin ]];
then
    PATH="$HOME/.local/bin:$PATH"
fi

# Composer global install
if [[ -d ~/.config/composer/vendor/bin/ ]];
then
    PATH="$PATH:$HOME/.config/composer/vendor/bin/"
fi

# RVM bin folder
if [[ -d ~/.rvm/bin ]];
then
    PATH=$PATH:$HOME/.rvm/bin
fi

# Settings for interactive shell only inside this block
if [[ $- == *i* ]]
then

    # shellcheck source=/dev/null
    source /var/local/ps1-prompt

    #Prevent Ctrl+S Freezing things
    stty -ixon

    # fix spelling errors for cd, only in interactive shell
    shopt -s cdspell

    # More useful bash completelion setting
    bind "set completion-ignore-case on" # note: bind used instead of sticking these in .inputrc
    bind "set bell-style none" # no bell
    bind "set show-all-if-ambiguous On" # show list automatically, without double tab

    alias gti=git

    complete -r cd 2>/dev/null  # completion on symlinks is unusual and a __complete__ pain in the arse. Let's remove it

    export EDITOR=vim
    alias vi="vim"

    # Docker Node stuff
    DOCKER_NODE_VER=${DOCKER_NODE_VER:-16}
    docker-node-version() {
      case "${1:-}" in
        -s|--set)
          if [ "$2" ]; then
              DOCKER_NODE_VER="$2"
            echo
            echo "docker-node will now use node:$2 image!"
          fi
          return 0
      esac
      echo "$DOCKER_NODE_VER"
    }
    docker-node-image() {
      echo -n "node:$(docker-node-version "$@")"
    }
    docker-node-run() {
    set -x
      local dp
      [ "$1" = "bash" ] && dp="-it"
      dp="$dp --rm"
      dp="$dp -v ${PWD}:/usr/src/app"
      dp="$dp -w /usr/src/app"
      # shellcheck disable=SC2086
      docker run $dp "$(docker-node-image "$@")" "$@"
    set +x
    }
    dnode() { docker-node-run "$@"; }
    dnpm() { docker-node-run npm "$@"; }
    dnpx() { docker-node-run npx "$@"; }
    dyarn() { docker-node-run yarn "$@"; }

fi
