# Compiles all of the docs in docs/build.
all: build/slate _jsdoc.erb
	cd build/slate && \
	cp -r ${CURDIR}/source/* source/ && \
	cp -r ${CURDIR}/*md source/ && \
	cp ${CURDIR}/_jsdoc.erb source/ && \
	cp ${CURDIR}/build/jsdoc_template/stylesheets/jsdoc.css source/stylesheets/ && \
	bundle exec middleman build --clean && \
	cp -r build/* ${CURDIR}/build/ && \
	echo "docs successfully compiled to HTML. To view docs, run:" && \
	echo "  open build/index.html"

clean:
	rm _jsdoc.erb
	rm -rf build

# Generate the JavaScript documentation by using jsdoc and Kelda's template.
_jsdoc.erb: build/jsdoc_template ../blueprint/
	build/jsdoc_template/node_modules/.bin/jsdoc \
		-t build/jsdoc_template/ \
		$(abspath ${CURDIR}/../js/bindings/bindings.js) -d ./

build/jsdoc_template:
	mkdir -p build && \
	cd build && \
	git clone https://github.com/kelda/jsdoc_template.git && \
	cd jsdoc_template && \
	npm install .

build/slate:
	mkdir -p build && \
	cd build && \
	git clone https://github.com/kelda/slate.git && \
	cd slate && \
	bundle install
