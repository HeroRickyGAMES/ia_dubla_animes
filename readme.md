<h2>Gerenciador de dublagem com IA focado em Animes</h2>

Esse repositorio gera um banco de vozes focado para CPU!

````
git clone https://github.com/HeroRickyGAMES/ia_dubla_animes.git
````
Ativar o venv

Windows:
````
./venv/bin/activate
````

Linux ou Mac
````
source venv/bin/activate
````

Precisa do ````ffmpeg```` para fazer o video final

No Windows é só colocar no Google e baixar o ````ffmpeg````

Mas para baixar para Linux use esse comando
````sudo apt update && sudo apt install ffmpeg -y````

Como no projeto eu coloquei o ````venv```` não precisa instalar as dependencias!

Mas qualquer dificuldade vc pode instalar essess scripts

Processamento e Edição de Vídeo/Áudio:

moviepy: Usada para extrair o áudio do vídeo original e, posteriormente, para compor o vídeo final com o áudio dublado.

pydub: Utilizada para manipulação de segmentos de áudio, como a combinação de amostras de voz para a criação da impressão digital do locutor.

audiostretchy: Empregada para aplicar time-stretching (ajuste de tempo) nos segmentos de áudio dublado, garantindo a sincronização com a duração original do segmento no vídeo.

soundfile e numpy: Usadas para manipulação de dados de áudio em formato de array, como na criação de arquivos de silêncio.

Modelos de Inteligência Artificial e Tarefas de Voz:

faster-whisper: Implementa o modelo Whisper otimizado para a transcrição do áudio.

pyannote.audio e pyannote.core: Utilizadas para Diarização de Locutor (separação por voz) e para a criação das Impressões Digitais (embeddings) dos locutores.

TTS (Coqui TTS): Biblioteca para a Síntese de Voz (Text-to-Speech).

torch (PyTorch): A principal biblioteca de aprendizado de máquina, usada como backend para os modelos de Diarização, Embedding de Voz e TTS.

Tradução e Utilidades:

googletrans: Biblioteca usada para realizar a tradução do texto transcrito.

yaml: Utilizada para carregar o arquivo de configuração do projeto (config.yaml).

Para executar execute

````
python main.py video.mp4 --config .\config.yaml
````

OOOU
você pode usar o

````
.\dublar_todos.ps1
````

Mas para isso vc precisa jogar os videos na pasta ````/videos````

É isso! Foi feito com muito amor e carinho, um pouco de tilt também  mas ekekekkekeke

Quer ajudar? Faça uma PR ou abra uma Issue!

Status do projeto:

✅    Dublando

✅    Aceitando Banco de Vozes

✅❌ Entendendo o que é homem ou mulher, criança ou velho

✅❌    Entendendo expressões

✅    Sincrono
