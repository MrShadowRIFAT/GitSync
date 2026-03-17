import os
import time
import requests
import git
from github import Github
from github.GithubException import RateLimitExceededException, GithubException
from backend.config import load_settings, save_settings
from backend.database import log_action

class GitHubService:
    def __init__(self):
        self.refresh_token()

    def refresh_token(self):
        self.settings = load_settings()
        self.token = self.settings.get("github_token")
        self.gh = Github(self.token) if self.token else None

    def execute_with_retry(self, func, *args, **kwargs):
        while True:
            try:
                return func(*args, **kwargs)
            except RateLimitExceededException:
                log_action("WARNING", "GitHub API Rate limit exceeded. Waiting 60 seconds...", "GitHubService")
                time.sleep(60)
            except Exception as e:
                log_action("ERROR", f"GitHub API Error: {e}", "GitHubService")
                raise e

    def get_user_profile(self):
        if not self.token:
            return None
        headers = {
            "Authorization": f"token {self.token}",
            "Accept": "application/vnd.github.v3+json"
        }
        try:
            resp = requests.get("https://api.github.com/user", headers=headers)
            if resp.status_code == 200:
                data = resp.json()
                profile = {
                    "login": data.get("login"),
                    "email": data.get("email"),
                    "avatar_url": data.get("avatar_url")
                }
                settings = load_settings()
                settings["github_profile"] = profile
                save_settings(settings)
                return profile
            else:
                log_action("ERROR", f"Failed to fetch user profile: {resp.status_code}", "GitHubService")
        except Exception as e:
            log_action("ERROR", f"Exception fetching user profile: {e}", "GitHubService")
        return None

    def get_user_repos(self):
        if not self.gh:
            log_action("ERROR", "GitHub Token is missing. Please add it in Settings.", "GitHubService")
            return []
        user = self.execute_with_retry(self.gh.get_user)
        repos = self.execute_with_retry(user.get_repos)
        return list(repos)
        
    def create_repo(self, name, private=True):
        if not self.gh:
            log_action("ERROR", "GitHub Token is missing. Remote repo creation aborted.", "GitHubService")
            return None
        try:
            user = self.execute_with_retry(self.gh.get_user)
            repo = self.execute_with_retry(user.create_repo, name, private=private, auto_init=False)
            log_action("INFO", f"Created new GitHub repository: {name} (Private: {private})", "GitHubService")
            return repo
        except GithubException as e:
            if e.status == 422: # Already exists
                log_action("WARNING", f"Repo {name} creation failed (mostly likely exists).", "GitHubService")
                user = self.execute_with_retry(self.gh.get_user)
                return self.execute_with_retry(user.get_repo, name)
            raise e
        
    def delete_repo(self, repo_name):
        if not self.gh:
            log_action("ERROR", "GitHub Token is missing. Cannot automatically delete remote repo.", "GitHubService")
            return False
        try:
            user = self.execute_with_retry(self.gh.get_user)
            repo = self.execute_with_retry(user.get_repo, repo_name)
            self.execute_with_retry(repo.delete)
            log_action("INFO", f"Deleted repository: {repo_name}", "GitHubService")
            return True
        except Exception as e:
            log_action("ERROR", f"Failed to delete {repo_name}: {e}", "GitHubService")
            return False

    def clone_repo(self, clone_url, local_path):
        if "://" in clone_url and self.token:
            auth_url = clone_url.replace("https://", f"https://{self.token}@")
        else:
            auth_url = clone_url
        
        try:
            log_action("INFO", f"Cloning {clone_url} to {local_path}", "GitHubService")
            git.Repo.clone_from(auth_url, local_path)
            return True
        except Exception as e:
            log_action("ERROR", f"Failed to clone repo {clone_url}: {e}", "GitHubService")
            return False
            
    def init_and_push(self, local_path, repo_url):
        try:
            if "://" in repo_url and self.token:
                auth_url = repo_url.replace("https://", f"https://{self.token}@")
            else:
                auth_url = repo_url
                
            repo = git.Repo.init(local_path)
            # Check if remote exists
            if "origin" not in [r.name for r in repo.remotes]:
                repo.create_remote("origin", auth_url)
            else:
                repo.remotes.origin.set_url(auth_url)
                
            repo.git.add(A=True)
            try:
                repo.index.commit("Initial commit from GitSync")
            except:
                pass # Already committed
            
            # Switch to main branch if master or others to ensure modern standards
            if "main" not in [h.name for h in repo.heads]:
                repo.git.checkout("-b", "main")
            
            repo.remotes.origin.push(refspec="refs/heads/main:refs/heads/main", set_upstream=True)
            log_action("INFO", f"Initialized and pushed {local_path} to {repo_url}", "GitHubService")
            return True
        except Exception as e:
            log_action("ERROR", f"Failed to init/push {local_path}: {e}", "GitHubService")
            return False
