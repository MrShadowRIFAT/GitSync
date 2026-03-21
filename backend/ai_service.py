import os
import google.generativeai as genai
from openai import OpenAI
from backend.config import load_settings
from backend.database import log_action

class AIService:
    def __init__(self):
        self.refresh_keys()
        
    def refresh_keys(self):
        self.settings = load_settings()
        self.openai_key = self.settings.get("openai_key")
        self.gemini_key = self.settings.get("gemini_key")
        
        self.gemini_model = None
        self.openai_client = None
        
        if self.gemini_key:
            try:
                genai.configure(api_key=self.gemini_key)
                self.gemini_model = genai.GenerativeModel('gemini-1.5-flash')
            except Exception as e:
                log_action("ERROR", f"Gemini Init Error: {e}", "AIService")
        
        if self.openai_key:
            try:
                self.openai_client = OpenAI(api_key=self.openai_key)
            except Exception as e:
                log_action("ERROR", f"OpenAI Init Error: {e}", "AIService")

        groq_api_key = None
        try:
            import sys
            import json
            base_path = getattr(sys, '_MEIPASS', os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
            secrets_path = os.path.join(base_path, 'backend', 'secrets.json')
            if os.path.exists(secrets_path):
                with open(secrets_path, 'r') as f:
                    secrets = json.load(f)
                    groq_api_key = secrets.get('GROQ_API_KEY')
        except Exception:
            pass

        if groq_api_key:
            try:
                self.groq_client = OpenAI(
                    api_key=groq_api_key,
                    base_url="https://api.groq.com/openai/v1"
                )
            except Exception:
                self.groq_client = None
        else:
            self.groq_client = None

    def _generate(self, prompt):
        self.refresh_keys() # Always check for updated keys
        try:
            if self.openai_client:
                response = self.openai_client.chat.completions.create(
                    model="gpt-3.5-turbo",
                    messages=[{"role": "user", "content": prompt}],
                    max_tokens=300
                )
                return response.choices[0].message.content.strip()
            elif self.gemini_model:
                response = self.gemini_model.generate_content(prompt)
                return response.text.strip()
            elif self.groq_client:
                response = self.groq_client.chat.completions.create(
                    model="llama3-8b-8192",
                    messages=[{"role": "user", "content": prompt}],
                    max_tokens=300
                )
                return response.choices[0].message.content.strip()
        except Exception as e:
            log_action("ERROR", f"AI Generation Error: {e}", "AIService")
        return None

    def generate_readme(self, language, project_name):
        self.refresh_keys()
        template = self.settings.get("readme_template", "").strip()
        if template:
            prompt = f"Generate a README.md for a {language} project named '{project_name}'. Use the following template exactly as the structure and fill in the blanks/details accordingly:\n\n{template}\n\nDo not include any conversational text, just the markdown."
        else:
            prompt = f"Generate a brief, professional README.md for a {language} project named '{project_name}'. Do not include any conversational text, just the markdown."
        content = self._generate(prompt)
        return content if content else f"# {project_name}\n\nAuto-generated README."

    def generate_gitignore(self, language):
        prompt = f"Generate a standard .gitignore file for a {language} project. Just output the content, no markdown blocks if possible."
        content = self._generate(prompt)
        if content:
            if content.startswith("```"):
                lines = content.split('\n')
                content = '\n'.join(lines[1:-1]) if len(lines) > 2 else content
            return content
        
        # Fallbacks
        if language.lower() == "python":
            return "__pycache__/\n.venv/\n*.pyc\n"
        elif language.lower() in ["node", "javascript", "typescript"]:
            return "node_modules/\ndist/\n.env\n"
        return ""

    def generate_commit_message(self, diff):
        prompt = f"Generate a concise git commit message (1 line) for the following diff. Only return the commit message, no quotes or prefix:\n\n{diff[:1500]}"
        content = self._generate(prompt)
        return content if content else "Auto sync update"
