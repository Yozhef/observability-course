# Виконується всередині контейнера glitchtip через: ./manage.py shell < цей_файл
# Ідемпотентно створює user + org + team + project і друкує DSN=...
# Використовує django.apps.get_model, щоб не залежати від шляхів модулів між версіями.
import importlib

from django.apps import apps
from django.contrib.auth import get_user_model

EMAIL = "admin@course.local"
PASSWORD = "course-demo-123"

U = get_user_model()
user = U.objects.filter(email=EMAIL).first()
if not user:
    user = U.objects.create_user(email=EMAIL, password=PASSWORD)
user.is_staff = True
user.is_superuser = True
user.save()

# верифікуємо email, щоб логін-форма пускала одразу
try:
    EmailAddress = apps.get_model("account", "EmailAddress")
    ea, _ = EmailAddress.objects.get_or_create(
        user=user, email=EMAIL, defaults={"verified": True, "primary": True})
    if not ea.verified:
        ea.verified = True
        ea.save()
except Exception as e:
    print("emailaddress:", e)

Organization = apps.get_model("organizations_ext", "Organization")
org = Organization.objects.filter(slug="course").first()
if not org:
    org = Organization.objects.create(name="course", slug="course")

org_user = None
try:
    role_owner = None
    try:
        mod = importlib.import_module(Organization.__module__)
        role_enum = getattr(mod, "OrganizationUserRole", None)
        if role_enum is not None:
            role_owner = role_enum.OWNER
    except Exception:
        pass
    if not org.users.filter(id=user.id).exists():
        if role_owner is not None:
            org_user = org.add_user(user, role=role_owner)
        else:
            org_user = org.add_user(user)
    else:
        org_user = org.organization_users.filter(user=user).first()
except Exception as e:
    print("org membership:", e)

Project = apps.get_model("projects", "Project")
project = Project.objects.filter(organization=org, slug="demo-app").first()
if not project:
    project = Project.objects.create(
        name="demo-app", slug="demo-app", organization=org, platform="go")

try:
    Team = apps.get_model("teams", "Team")
    team = Team.objects.filter(organization=org, slug="demo").first()
    if not team:
        team = Team.objects.create(organization=org, slug="demo")
    if org_user is not None and not team.members.filter(id=org_user.id).exists():
        team.members.add(org_user)
    team.projects.add(project)
except Exception as e:
    print("team:", e)

key = None
try:
    ProjectKey = apps.get_model("projects", "ProjectKey")
except LookupError:
    ProjectKey = None
    # пошукаємо модель ProjectKey у будь-якому app
    for m in apps.get_models():
        if m.__name__ == "ProjectKey":
            ProjectKey = m
            break
if ProjectKey is None:
    print("key: ProjectKey model not found")
else:
    key = ProjectKey.objects.filter(project=project).first()
    if not key:
        key = ProjectKey.objects.create(project=project)

if key is not None:
    pub = key.public_key
    pub = pub.hex if hasattr(pub, "hex") else str(pub).replace("-", "")
    print(f"DSN=http://{pub}@glitchtip:8000/{project.id}")
