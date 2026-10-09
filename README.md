# Kumpanposten

En intern tidning med Slack-bidrag, AI-bearbetning och manuell redigering. Go-servern använder SQLite och serverrenderade HTML-mallar. Den aktiva appen består av `cmd/server`, `internal/kp` och `internal/auth`. HTML-mallarna i `internal/kp/views` bäddas in i binären; `static/` behövs vid körning.

Appen startar med ett nytt schema i `kp.db`, separat från gamla `newsletter.db`. Ingen äldre data importeras. Äldre interna paket ligger kvar men är inte del av den nya appen. Det finns ingen `/pp`-kompatibilitet. Intranätaktiviteter och födelsedagar är uppskjutna.

## Kom igång

Använd Go 1.23 eller senare och en C-kompilator för SQLite via CGO. Exportera miljövariablerna nedan innan start. Appen läser bara processens miljö, inte `.env` eller andra dotenv-filer. Saknad eller ogiltig konfiguration stoppar starten; felmeddelandet innehåller bara variabelns namn.

```sh
go run ./cmd/server
curl --fail http://localhost:8080/health
```

Lokal utveckling kan använda `BASE_URL=http://localhost:8080`. Slack behöver en publik HTTPS-adress för kommandon och en registrerad callback för inloggning. Produktionsstart behöver riktig konfiguration och utgående HTTPS till Slack och OpenAI.

## Miljövariabler

| Variabel | Krav eller standard |
| --- | --- |
| `PORT` | `8080`; heltal 1–65535. |
| `DATABASE_PATH` | `kp.db` när variabeln saknas. Explicit tomt värde avvisas. I Docker är standarden `/data/kp.db`. |
| `BASE_URL` | Sätt till `https://kumpanbladet.kumpan.tech` i produktion. HTTP tillåts bara för `localhost` och `127.0.0.1` vid utveckling. Avslutande `/` tas bort. Ingen annan sökväg, query, användarinfo eller fragment tillåts. |
| `SLACK_WORKSPACE_ID` | Obligatoriskt ID för den tillåtna arbetsytan. |
| `SLACK_SIGNING_SECRET` | Obligatorisk signeringshemlighet från Slack-appen. |
| `SLACK_BOT_TOKEN` | Obligatorisk bot-token från installationen. |
| `SLACK_CLIENT_ID` | Obligatoriskt klient-ID för Sign in with Slack. |
| `SLACK_CLIENT_SECRET` | Obligatorisk klienthemlighet för Sign in with Slack. |
| `SESSION_SECRET` | Obligatorisk slumpmässig hemlighet på minst 32 byte. |
| `OPENAI_API_KEY` | Obligatorisk OpenAI API-nyckel. |
| `OPENAI_MODEL` | `gpt-4.1-mini`; kan ersättas med en modell som stöder Responses API och Structured Outputs. |
| `OPENAI_IMAGE_MODEL` | `gpt-image-2.5-flare`; bildmodell för Images API. Använder samma `OPENAI_API_KEY` som textreportern. |
| `SLACK_PUBLISH_CHANNEL` | Obligatoriskt kanal-ID för publiceringslänkar. Bjud in boten till kanalen. |
| `ADMIN_USERS` | Obligatorisk kommaseparerad lista med minst ett Slack-användar-ID. Mellanslag runt ID:n tas bort och dubbletter slås ihop. |

Tom `PORT`, `OPENAI_MODEL` och `OPENAI_IMAGE_MODEL` använder standardvärdena. Obligatoriska värden, hemligheter och modellnamn får inte innehålla whitespace. Lägg inte hemligheter i versionshanteringen. `UNSPLASH_ACCESS_KEY` används inte längre och kan tas bort från Coolify.

## Slack-app

Skapa en app från `slack-app-manifest.json`. Manifestet använder produktionsdomänen `https://kumpanbladet.kumpan.tech`. Installation och behörighetsgodkännande görs manuellt i rätt arbetsyta.

- `/tellkp <rapport>` sparar en rapport med avsändarens Slack-namn.
- `/askkp <fråga>` sparar frågan utan avsändar-ID eller namn i appen.
- Båda kommandona skickas till `https://kumpanbladet.kumpan.tech/api/slack/commands`.
- Bot-scopes är `commands`, `chat:write` och `im:write`. Den sista behövs för direktmeddelanden till redaktörer.
- Registrera `https://kumpanbladet.kumpan.tech/auth/callback` som redirect URL och kontrollera appens Sign in with Slack-inställningar. Inloggningen använder user-scopes `openid` och `profile`, inte `email`.

Bot-installation och Sign in with Slack är separata OAuth-flöden i samma app. Slack tillåter inte att OpenID-scopes blandas med bot-scopes i samma auktoriseringsbegäran. `/auth/callback` hanterar inloggning, inte bot-installation. Ingen Events API eller interactivity behövs.

Den nya appen behöver inte äldre scopes för att läsa kanaler, omnämnanden, direktmeddelanden eller användarprofiler. User-scope `im:write` behövs inte heller. Att ta bort scopes ur konfigurationen återkallar inte nödvändigtvis befintliga tokens behörigheter; full återkallelse kräver separat hantering av den gamla installationen.

## Redaktion och integritet

Bidrag bearbetas av OpenAI till utkast via Responses API med Structured Outputs. Anropen använder `store=false`, så svaren sparas inte för senare hämtning via API:t. Detta är inte ett löfte om noll datalagring hos leverantören; OpenAI:s datavillkor och kontoinställningar gäller fortfarande. Rapporter skickas med rapporttext och avsändarnamn. Frågor skickas med frågetext men utan separat avsändarmetadata. Texten kan fortfarande innehålla identifierande uppgifter. Slack känner till avsändaren när kommandot skickas, även för `/askkp`.

Redaktören granskar AI-texten, rättar fakta och tar manuellt bort namn och andra identifierande detaljer före publicering. AI-anonymisering är ingen garanti. Misslyckad bearbetning kräver att redaktören väljer ett nytt försök eller tar bort artikeln.

Nyhetsartiklar skrivs ur reporterns perspektiv. Kollegan är en källa som kan nämnas i berättelsen, inte författare eller avsändare i en byline. Anonyma frågor samlas efter nyheterna under rubriken "Inuti Kumpanernas kroppar och knoppar". Varje fråga får en påhittad brevskrivarsignatur som visas tillsammans med frågan, före reporterns svar.

## Artikelbilder

Nya bilder genereras helt av OpenAI. Sätt `OPENAI_IMAGE_MODEL=gpt-image-2.5-flare` som runtime-variabel i Coolify och låt `OPENAI_MODEL` fortsätta välja textmodell, exempelvis `gpt-4.1-mini`. Projektets API-nyckel måste ha tillgång till både text- och bildmodellen. Ingen Unsplash-integration eller bildredigering av stockfoton görs.

Textreportern skriver en konkret engelsk bildprompt tillsammans med artikeln. Scenen får en liten visuell poäng kopplad till berättelsen, exempelvis ett äpple med ett bettmärke bredvid en presentationsdator eller juldekorationer på en ny bil. Prompten ska undvika personnamn, kunduppgifter och andra identifierande detaljer. Redaktionell granskning behövs fortfarande; AI-instruktioner är ingen garanti för lämpligt innehåll.

Workern anropar Images API i bakgrunden med en bild per artikel, kvalitet `medium` och storlek `1536x1024`. Nyheter får svartvit tidningsfotografi med lätt kornighet. Frågespalten får enkla svarta linjeteckningar utan färg, skuggning eller text, med transparent bakgrund så att det gula fältet syns igenom. Bildfilerna bearbetas även lokalt: nyhetsbilder sparas som gråskale-JPEG och teckningar som svart PNG med bevarad transparens.

Bildfel blockerar inte färdiga artiklar eller publicering. Redaktören kan ändra bildprompten, generera en ny bild eller ta bort bilden. När en ersättning misslyckas ligger den tidigare bilden kvar. Varje nytt försök kan medföra en ny API-kostnad. Avbrutna eller osäkra bildförsök markeras som misslyckade vid omstart och kräver ett manuellt nytt försök, så att en potentiellt redan debiterad begäran inte upprepas automatiskt. En färdig, sparad bild kan kopplas till artikeln även om avstängning precis har inletts. Textredigering under bildgenerering utlöser inte en extra bildbeställning.

Bilder sparas i katalogen `images` bredvid databasen, alltså `/data/images` med Docker-standardvärdet. Den befintliga `/data`-volymen lagrar både databas och bilder. Varje bild får ett unikt namn och varianter på 400 och 800 pixlar. Filinnehåll och katalogposter synkas innan databasen refererar till dem. Sidvisningar gör inga nya genereringsanrop. Bildrutten kräver Slack-inloggning; utkastbilder kräver dessutom redaktörsbehörighet. Filkatalogen exponeras inte direkt som statisk lagring.

Befintliga texter och publicerade bildval bevaras. För äldre utkast anger redaktören en scen i **Bildprompt** och väljer **Generera bild**. Tidigare Unsplash-bilder kan ligga kvar tills de ersätts, och visas fortsatt med sina obligatoriska fotograf-/Unsplash-länkar. Ingen Unsplash-sökning eller download-tracking sker längre. Nya genererade bilder får inga fotografkrediter eller "Illustrationsbild"-etiketter.

Refererade bilder skrivs aldrig över. När en bild ersätts eller tas bort slutar den gamla filen att vara åtkomlig via appen, men filen behålls på disk. Automatisk rensning av äldre filer ingår inte. Misslyckade eller föråldrade nya resultat som säkert inte refereras av databasen kan städas bort direkt.

Fredagar från kl. 09.00 i `Europe/Stockholm` får redaktörer en påminnelse om inget nummer har publicerats den dagen. Ingen automatisk publicering sker. Redaktörer kan publicera manuellt vilken dag som helst när alla kvarvarande artiklar är färdiga. Publicerat innehåll är oföränderligt; arkivets utseende kan ändras när HTML-mallarna ändras.

Slack-notiser levereras minst en gång. En tappad nätverksbekräftelse eller ett avbrott efter sändning kan i sällsynta fall ge dubbla notiser, men inte dubbel publicering.

Inloggningen tillåter bara den konfigurerade Slack-arbetsytan. Sessioner gäller i åtta timmar. En redan utfärdad session kontrolleras inte löpande mot Slack; omedelbar avprovisionering är inte implementerad. För administrativ återkallelse behöver sessionen löpa ut eller `SESSION_SECRET` roteras och servern startas om. Rotation ogiltigförklarar alla sessioner.

## HTTP-rutter

| Metod och sökväg | Användning |
| --- | --- |
| `GET /health` | Hälsokontroll utan inloggning. |
| `POST /api/slack/commands` | Signerade Slack-kommandon från rätt arbetsyta. |
| `GET /auth/login` | Starta Slack-inloggning. |
| `GET /auth/callback` | OpenID Connect-callback. |
| `POST /auth/logout` | Logga ut med formulärets CSRF-token. |
| `GET /` | Senaste publicerade numret, för inloggade läsare. |
| `GET /archive` | Arkiv för inloggade läsare. |
| `GET /issues/{id}` | Publicerat nummer för inloggade läsare. |
| `GET /images/{id}/{name}` | Genererad bild kopplad till artikeln. Publicerade bilder för läsare; utkastbilder endast för redaktörer. `w=400`, `800` eller `1536` väljer en bildvariant. |
| `GET /draft` | Aktuellt utkast, endast redaktörer i `ADMIN_USERS`. |
| `GET /editor/article/{id}` | Redigera artikel. |
| `POST /editor/article/{id}/save` | Spara redigering. |
| `POST /editor/article/{id}/remove` | Ta bort artikel från utkastet. |
| `POST /editor/article/{id}/retry` | Försök bearbeta en misslyckad artikel igen. |
| `POST /editor/article/{id}/image` | Köa en bildgenerering med en redigerbar scenprompt. |
| `POST /editor/article/{id}/image/remove` | Ta bort bilden och avbryt väntande bildgenerering. |
| `POST /editor/issues/{id}/publish` | Bekräfta och publicera nummer. |

Alla `/editor/`-rutter kräver redaktörsbehörighet. POST-formulär kräver `csrf_token`; artikeländringar kräver även aktuell `revision`, och publicering kräver `confirm=yes` samt utkastets `review`-värde. Om artiklar har tillkommit eller ändrats sedan utkastet öppnades måste redaktören granska det igen. Detta är formulärrutter, inte ett separat publikt JSON-API.

## Docker och drift

```sh
docker build -t kp .
docker volume create kp-data
docker run --name kp --env-file /sökväg/till/kp.env \
  -p 8080:8080 -v kp-data:/data kp
```

Miljöfilen används här av Docker, inte av appen. Kör bakom en HTTPS-proxy med `BASE_URL` satt till den externa originen. Hälsokontrollen använder `PORT` och `/health`; ändra även portmappningen om du ändrar `PORT`.

Containern kör som `kp`, UID/GID 10001, i `/app`. Montera beständig lagring på `/data`; en bind-mount måste vara skrivbar för UID 10001. SQLite skapar även WAL/SHM-filer i samma katalog. Endast binären och `static/` kopieras till runtime, inga gamla migrationer eller mallkataloger.

Kör exakt en instans med en worker och schemaläggare. Flera repliker och överlappande rolling deploy är inte stödda. Stoppa den gamla instansen innan den nya startas mot samma databas.

Inloggningsstarten begränsas till 60 försök per minut och nätverkspeer. Bakom en reverse proxy delar användarna den gränsen. Appen litar inte på godtyckliga forwarded-headers. Konfigurera även per-klient-rate-limit i den betrodda proxyn före publik drift, så att en klient inte kan förbruka den gemensamma gränsen.

## Säkerhetskopiering

SQLite CLI finns i containern. Skapa en läskonsistent backup med SQLite, inte med rå `cp` av en öppen databas:

```sh
docker exec kp sqlite3 /data/kp.db '.backup /data/kp-backup.db'
docker cp kp:/data/kp-backup.db ./kp-backup.db
docker cp kp:/data/images ./kp-backup-images
```

Kopiera databasen först och bildkatalogen därefter; bilderna är oföränderliga, så katalogkopian innehåller även alla filer som databasögonblicket refererar till. Flytta båda delarna till skyddad lagring utanför servern och prova återställning mot en separat instans. Backuper innehåller privata bidrag, frågor och bilder. Återställ båda delarna med appen stoppad och rätt ägare, UID/GID 10001 i Docker. Blanda aldrig återställd databas med gamla WAL/SHM-filer. Vid filbaserad kopiering av en stoppad installation måste hela SQLite-katalogen, inklusive eventuella WAL-filer, följa med. Inga backup- eller återställningsskript körs automatiskt.

Den här versionen uppgraderar KP-databasen från schema 1 eller 2 till 3 vid start, i en transaktion. Befintligt innehåll och publiceringstidpunkter bevaras. Gamla ofärdiga Unsplash-jobb avbryts utan att skapa betalda AI-bildbeställningar från tidigare sökord. Ta en backup före deploy. Äldre appversioner kan inte öppna schema 3; en rollback till dem kräver en kompatibel backup, inte bara byte av containerimage.

## Utveckling

```sh
go test ./internal/kp ./internal/auth ./cmd/server
go build -o /tmp/kp ./cmd/server
```

Konfigurationstester isolerar miljön med `t.Setenv` och använder inga riktiga nycklar eller nätverksanrop. Äldre paket och tester är inte den aktiva appens verifiering; vissa äldre tester behöver externa tjänster.
