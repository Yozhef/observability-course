# Виконується всередині контейнера glitchtip через: ./manage.py shell < цей_файл
# Ідемпотентно створює user + org + team + project і друкує DSN=...
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
    from allauth.account.models import EmailAddress
    ea, _ = EmailAddress.objects.get_or_create(
        user=user, email=EMAIL, defaults={"verified": True, "primary": True})
    if not ea.verified:
        ea.verified = True
        ea.save()
except Exception as e:
    print("emailaddress:", e)

from organizations_ext.models import Organization
org = Organization.objects.filter(slug="course").first()
if not org:
    org = Organization.objects.create(name="course", slug="course")

org_user = None
try:
    from organizations_ext.models import OrganizationUserRole
    if not org.users.filter(id=user.id).exists():
        org_user = org.add_user(user, role=OrganizationUserRole.OWNER)
    else:
        org_user = org.organization_users.filter(user=user).first()
except Exception as e:
    print("org membership:", e)

from projects.models import Project
project = Project.objects.filter(organization=org, slug="demo-app").first()
if not project:
    project = Project.objects.create(
        name="demo-app", slug="demo-app", organization=org, platform="go")

try:
    from teams.models import Team
    team = Team.objects.filter(organization=org, slug="demo").first()
    if not team:
        team = Team.objects.create(organization=org, slug="demo")
    if org_user and not team.members.filter(id=org_user.id).exists():
        team.members.add(org_user)
    team.projects.add(project)
except Exception as e:
    print("team:", e)

try:
    from projects.models import ProjectKey
    key = ProjectKey.objects.filter(project=project).first()
    if not key:
        key = ProjectKey.objects.create(project=project)
except Exception as e:
    print("key via projects:", e)
    key = None
if key is None:
    try:
        from api_tokens.models import ProjectKey  # fallback for other layouts
        key = ProjectKey.objects.filter(project=project).first() or ProjectKey.objects.create(project=project)
    except Exception as e:
        print("key fallback:", e)

pub = key.public_key
pub = pub.hex if hasattr(pub, "hex") else str(pub).replace("-", "")
print(f"DSN=http://{pub}@glitchtip:8000/{project.id}")
