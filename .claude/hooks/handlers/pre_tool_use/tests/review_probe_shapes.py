SHAPES = r'''
{C}
echo x $({C})
echo "$({C})"
grep "$(sudo {C})" f
echo `{C}`
cat <({C})
echo hi > >({C})
echo x; {C}
echo x && {C}
echo x | {C}
true || {C}
echo '{C}' > s.sh && bash s.sh
echo '{C}'>s.sh;sh s.sh
echo '{C}' >> scripts/git-hooks/pre-commit && git commit -m x
printf '%s\n' '{C}' >> scripts/git-hooks/pre-commit; git commit -qm x
echo '{C}' >> .git/hooks/pre-commit && git commit -m x
echo '{C}' >> scripts/git-hooks/pre-push && git push origin HEAD
echo '{C}' >> scripts/git-hooks/commit-msg; git commit -m y
cat >> scripts/git-hooks/pre-commit <<'EOF'
{C}
EOF
git commit -m x
echo '[core] sshCommand = {C}' >> .git/config && git fetch
echo '[core] fsmonitor = {C}' >> .git/config; git status
echo '[filter "x"] clean = {C}' >> .git/config && echo '* filter=x' > .gitattributes && git add .
echo '[diff] external = {C}' >> .git/config && git diff
echo '[core] pager = {C}' >> .git/config && git log
echo '[alias] lg = !{C}' >> .git/config && git log
echo 'sudo {C}' >> ~/.bashrc
echo '{C}' >> ~/.bash_profile
echo '{C}' > ~/.config/autostart/x.desktop
git -c core.pager='{C}' log
git -c alias.x='!{C}' x
git -C . -c core.sshCommand='{C}' fetch
git --exec-path=/x log '{C}'
git grep -O'{C}' x
git grep --open-files-in-pager='{C}' x
git grep --open-files='{C}' x
git fetch --upload-pack='{C};' .
git fetch --upload='{C};' .
git push --receive='{C};' .
git push --exec='{C};' .
git fetch --upload-pa='{C};' .
git log --output=/tmp/x '{C}'
GIT_EXTERNAL_DIFF='{C}' git diff
GIT_SSH_COMMAND='{C}' git fetch
GIT_PAGER='{C}' git log
PAGER='{C}' git log
rg --pre='{C}' x
rg --pre '{C}' x
rg --pr='{C}' x
sort --compress-program='{C}' x
sort --compress='{C}' x
wc --files0-from=x '{C}'
jq -n '"{C}"'
jq -r '.a' <<< '{C}'
gh issue create --title t --body '{C}'
gh pr comment 1 --body-file x '{C}'
gh pr create -b '{C}' -t t
printf -v x '{C}'
echo '{C}'
printf '%s' '{C}'
grep -r '{C}' .
git commit -m '{C}'
git commit -m "x" -m '{C}'
git log --grep='{C}'
test -n '{C}'
[ -n '{C}' ]
cat <<'EOF'
{C}
EOF
cat <<EOF
{C}
EOF
for x in a b; do {C}; done
for x in a; do echo '{C}'; done
if true; then {C}; fi
if test -f x; then echo '{C}'; fi
cd /tmp && {C}
cd /tmp; echo '{C}' > a; bash a
echo '{C}' | sh
echo '{C}' | bash -s
echo '{C}' | sudo sh
cat x | sh
printf '{C}' | bash
echo '{C}' | xargs -I{} sh -c {}
eval '{C}'
. <(echo '{C}')
source <(echo '{C}')
bash -c '{C}'
sh -c "{C}"
sudo bash -c '{C}'
env {C}
timeout 5 {C}
nohup {C} &
watch {C}
script -c '{C}'
find . -exec {C} \;
python3 -c 'import os; os.system("{C}")'
perl -e 'system("{C}")'
make -E 'x:;{C}' x
ssh host '{C}'
echo x
( {C} )
{ {C}; }
echo x; ( {C} )
x=1 {C}
X='{C}'; $X
X='{C}'; eval $X
echo x # {C}
: '{C}'
true '{C}'
echo '{C}' > /etc/profile.d/x.sh
echo '{C}' | tee -a scripts/git-hooks/pre-commit && git commit -m x
cat > scripts/git-hooks/pre-commit < /dev/null; echo '{C}' >> scripts/git-hooks/pre-commit; git commit -m z
git add . && git commit -m '{C}' && git push
ls; {C}
pwd && {C}
wc -l x; {C}
uniq x | {C}
head x | sudo {C}
echo '{C}' >&2; tail x
echo 'x' > '{C}'
echo '{C}' >> .envrc && cd .
echo '{C}' > Makefile
echo "alias ls='{C}'" >> ~/.bashrc
echo '{C}'>~/.bashrc
cat <<EOF > scripts/git-hooks/pre-commit
#!/bin/sh
{C}
EOF
git commit -m x
'''
