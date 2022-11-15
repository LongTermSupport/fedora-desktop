### --main

# Only apply configs once even if sourced multiple times
if [[ -z ${lts_bash_tweaks_active+x} ]]; then
  export lts_bash_tweaks_active=1

  # User specific aliases and functions
  alias rm='rm -i'
  alias cp='cp -i'
  alias mv='mv -i'
  alias ll='ls -alh --color'

  # Make parent dirs if they are missing
  alias mkdir='mkdir -pv'

  # Handle UTF-8 with less
  export LESSCHARSET=utf-8

  #History
  shopt -s histappend
  shopt -s cmdhist
  HISTCONTROL=ignoredups
  export HISTFILESIZE=20000
  export HISTSIZE=10000
  export HISTIGNORE="&:ls:[bf]g:exit"

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

      #Prompt
      function redPrompt(){
          export PS1='\[\e[1m\]$PWD\[\e[0m\]'"\n\[\033[38;5;1m\]\u\[$(tput sgr0)\]\[\033[38;5;15m\]@\[$(tput sgr0)\]\[\033[38;5;9m\]\h\[$(tput sgr0)\] "
      }
      function bluePrompt(){
          export PS1='\[\e[1m\]$PWD\[\e[0m\]'"\n\[\033[38;5;32m\]\u\[$(tput sgr0)\]\[\033[38;5;15m\]@\[$(tput sgr0)\]\[\033[38;5;32m\]\h\[$(tput sgr0)\] "
      }
      if [[ "$(whoami)" == "root" ]]
      then
          redPrompt
      else
          bluePrompt
      fi

      #Prevent Ctrl+S Freezing things
      stty -ixon

      # fix spelling errors for cd, only in interactive shell
      shopt -s cdspell

      # More useful bash completelion setting
      bind "set completion-ignore-case on" # note: bind used instead of sticking these in .inputrc
      bind "set bell-style none" # no bell
      bind "set show-all-if-ambiguous On" # show list automatically, without double tab

      alias gti=git

      complete -r cd  # completion on symlinks is unusual and a __complete__ pain in the arse. Let's remove it

      export EDITOR=vim
      alias vi="vim"
      
      # Docker Node stuff
      DOCKER_NODE_VER=${DOCKER_NODE_VER:-16}
      docker-node-version() {
        case "$1" in
          -s|--set)
            if [ "$2" ]; then
                DOCKER_NODE_VER="$2"
              echo 
              echo "docker-node will now use node:$2 image!"
            fi
            return 0
        esac
        echo $DOCKER_NODE_VER
      }
      docker-node-image() {
        echo -n "node:$(docker-node-version)"
      }
      docker-node-run() {
      set -x
        local dp
        [ "$1" = "bash" ] && dp="-it"
        dp="$dp --rm"
        dp="$dp -v "$PWD":/usr/src/app"
        dp="$dp -w /usr/src/app"
        docker run $dp $(docker-node-image) "$@"
      set +x
      }
      node() { docker-node-run "$@"; }
      npm() { docker-node-run npm "$@"; }
      npx() { docker-node-run npx "$@"; }
      yarn() { docker-node-run yarn "$@"; }


      
  fi
fi
